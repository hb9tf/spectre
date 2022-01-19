package main

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/golang/glog"

	"github.com/hb9tf/spectre/export"
	"github.com/hb9tf/spectre/extraction"
	"github.com/hb9tf/spectre/sdr"

	// Blind import support for sqlite3 used by sqlite.go.
	_ "github.com/mattn/go-sqlite3"
)

var (
	listen   = flag.String("listen", ":8443", "")
	certFile = flag.String("certFile", "", "Path of the file containing the certificate (including the chained intermediates and root) for the TLS connection.")
	keyFile  = flag.String("keyFile", "", "Path of the file containing the key for the TLS connection.")
	storage  = flag.String("storage", "", "Storage solutions to use (one of: sqlite, mysql)")

	// SQLite
	sqliteFile = flag.String("sqliteFile", "/tmp/spectre", "File path of the sqlite DB file to use.")

	// MySQL
	mysqlServer       = flag.String("mysqlServer", "127.0.0.1:3306", "MySQL TCP server endpoint to connect to (IP/DNS and port).")
	mysqlUser         = flag.String("mysqlUser", "", "MySQL DB user.")
	mysqlPasswordFile = flag.String("mysqlPasswordFile", "", "Path to the file containing the password for the MySQL user.")
	mysqlDBName       = flag.String("mysqlDBName", "spectre", "Name of the DB to use.")
)

const (
	collectEndpoint = "/spectre/v1/collect"
	renderEndpoint  = "/spectre/v1/render"
)

type SpectreServer struct {
	Server  *http.Server
	DB      *sql.DB
	Samples chan sdr.Sample
}

func (s *SpectreServer) collectHandler(c *gin.Context) {
	samples := []sdr.Sample{}

	if err := c.BindJSON(samples); err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	for _, sample := range samples {
		s.Samples <- sample
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"sampleCount": len(samples),
	})
}

func (s *SpectreServer) renderHandler(c *gin.Context) {
	type queryParameters struct {
		sdr        string `binding:"required"`
		identifier string `binding:"required"`
		startFreq  int64
		endFreq    int64
		startTime  int64
		endTime    int64
		addGrid    string
		imgWidth   int
		imgHeight  int
		imageType  string
	}

	parsedQueryParameters := queryParameters{}
	c.BindQuery(&parsedQueryParameters)

	var startFreq int64 // default to the lowest possible frequency
	if parsedQueryParameters.startFreq != 0 {
		startFreq = parsedQueryParameters.startFreq
	}

	endFreq := int64(math.MaxInt64) // default to the maximum possible frequency
	if parsedQueryParameters.endFreq != 0 {
		endFreq = parsedQueryParameters.endFreq
	}

	var startTime time.Time // default to the earliest possible timestamp of a sample
	if parsedQueryParameters.startTime != 0 {
		startTime = time.Unix(0, parsedQueryParameters.startTime*1000000) // from milli to nano
	}

	endTime := time.Now() // default to the latest possible timestamp of a sample
	if parsedQueryParameters.endTime != 0 {
		endTime = time.Unix(0, parsedQueryParameters.endTime*1000000) // from milli to nano
	}

	addGrid := true
	if parsedQueryParameters.addGrid == "0" || parsedQueryParameters.addGrid == "false" {
		addGrid = false
	}

	var imgWidth int
	if parsedQueryParameters.imgWidth != 0 {
		imgWidth = parsedQueryParameters.imgWidth
	}

	var imgHeight int
	if parsedQueryParameters.imgHeight != 0 {
		imgHeight = parsedQueryParameters.imgHeight
	}

	result, err := extraction.Render(s.DB, &extraction.RenderRequest{
		Image: &extraction.ImageOptions{
			Height:  imgHeight,
			Width:   imgWidth,
			AddGrid: addGrid,
		},
		Filter: &extraction.FilterOptions{
			SDR:        parsedQueryParameters.sdr,
			Identifier: parsedQueryParameters.identifier,
			StartFreq:  startFreq,
			EndFreq:    endFreq,
			StartTime:  startTime,
			EndTime:    endTime,
		},
	})
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	buf := new(bytes.Buffer)
	switch strings.ToLower(parsedQueryParameters.imageType) {
	case "png":
		png.Encode(buf, result.Image)

	default:
		jpeg.Encode(buf, result.Image, &jpeg.Options{Quality: jpeg.DefaultQuality})
	}
	c.Data(http.StatusOK, "application/octet-stream", buf.Bytes())
}

func main() {
	ctx := context.Background()
	// Set defaults for glog flags. Can be overridden via cmdline.
	flag.Set("logtostderr", "false")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "1")
	// Parse flags globally.
	flag.Parse()

	// Exporter and storage setup
	var db *sql.DB
	var exporter export.Exporter
	switch strings.ToLower(*storage) {
	case "csv": // CSV is a silent option as it only exports data but can't be used to render.
		exporter = &export.CSV{}
	case "sqlite":
		var err error
		db, err = sql.Open("sqlite3", *sqliteFile)
		if err != nil {
			glog.Exitf("unable to open sqlite DB %q: %s", *sqliteFile, err)
		}
		exporter = &export.SQL{
			DB: db,
		}
	case "mysql":
		pass, err := ioutil.ReadFile(*mysqlPasswordFile)
		if err != nil {
			glog.Exitf("unable to read MySQL password file %q: %s\n", *mysqlPasswordFile, err)
		}
		cfg := mysql.Config{
			User:   *mysqlUser,
			Passwd: strings.TrimSpace(string(pass)),
			Net:    "tcp",
			Addr:   *mysqlServer,
			DBName: *mysqlDBName,
		}
		db, err = sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			glog.Exitf("unable to open MySQL DB %q: %s", *mysqlServer, err)
		}
		db.SetConnMaxLifetime(3 * time.Minute)
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(10)
		exporter = &export.SQL{
			DB: db,
		}
	default:
		glog.Exitf("%q is not a supported export method, pick one of: sqlite, mysql", *storage)
	}

	// Export samples.
	samples := make(chan sdr.Sample, 1000)
	go func() {
		if err := exporter.Write(ctx, samples); err != nil {
			glog.Fatal(err)
		}
	}()

	// Configure and run webserver.
	router := gin.Default()
	s := SpectreServer{
		Server: &http.Server{
			Addr:    *listen,
			Handler: router, // use `http.DefaultServeMux`
		},
		DB:      db,
		Samples: samples,
	}

	router.POST(collectEndpoint, s.collectHandler)
	router.GET(renderEndpoint, s.renderHandler)

	if *certFile != "" || *keyFile != "" {
		glog.Fatal(s.Server.ListenAndServeTLS(*certFile, *keyFile))
	} else {
		glog.Infoln("Resorting to serving HTTP because there was no certificate and key defined.")
		glog.Fatal(s.Server.ListenAndServe())
	}

	glog.Flush()
}
