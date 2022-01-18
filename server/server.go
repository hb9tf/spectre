package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

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

func (s *SpectreServer) collectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Collect endpoint requires POST request.", http.StatusBadRequest)
		return
	}
	samples := []sdr.Sample{}
	if err := json.NewDecoder(r.Body).Decode(&samples); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, sample := range samples {
		s.Samples <- sample
	}
}

func (s *SpectreServer) renderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Render endpoint requires GET request.", http.StatusBadRequest)
		return
	}

	sdr := r.URL.Query().Get("sdr")
	identifier := r.URL.Query().Get("identifier")

	var startFreq int64 // default to the lowest possible frequency
	startFreqParam := r.URL.Query().Get("startFreq")
	if f, err := strconv.ParseInt(startFreqParam, 10, 64); err == nil {
		startFreq = f
	}

	endFreq := int64(math.MaxInt64) // default to the maximum possible frequency
	endFreqParam := r.URL.Query().Get("endFreq")
	if f, err := strconv.ParseInt(endFreqParam, 10, 64); err == nil {
		endFreq = f
	}

	var startTime time.Time // default to the earliest possible timestamp of a sample
	startTimeParam := r.URL.Query().Get("startTime")
	if t, err := strconv.ParseInt(startTimeParam, 10, 64); err == nil {
		startTime = time.Unix(0, t*1000000) // from milli to nano
	}

	endTime := time.Now().Add(24 * time.Hour) // default to the latest possible timestamp of a sample
	endTimeParam := r.URL.Query().Get("endTime")
	if t, err := strconv.ParseInt(endTimeParam, 10, 64); err == nil {
		endTime = time.Unix(0, t*1000000) // from milli to nano
	}

	addGrid := true
	addGridParam := r.URL.Query().Get("addGrid")
	if addGridParam == "0" || strings.ToLower(addGridParam) == "false" {
		addGrid = false
	}

	var imgWidth int
	imgWidthParam := r.URL.Query().Get("imgWidth")
	if s, err := strconv.ParseInt(imgWidthParam, 10, 32); err == nil {
		imgWidth = int(s)
	}

	var imgHeight int
	imgHeightParam := r.URL.Query().Get("imgHeight")
	if s, err := strconv.ParseInt(imgHeightParam, 10, 32); err == nil {
		imgHeight = int(s)
	}

	result, err := extraction.Render(s.DB, &extraction.RenderRequest{
		Image: &extraction.ImageOptions{
			Height:  imgHeight,
			Width:   imgWidth,
			AddGrid: addGrid,
		},
		Filter: &extraction.FilterOptions{
			SDR:        sdr,
			Identifier: identifier,
			StartFreq:  startFreq,
			EndFreq:    endFreq,
			StartTime:  startTime,
			EndTime:    endTime,
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/octet-stream")
	switch strings.ToLower(r.URL.Query().Get("imageType")) {
	case "png":
		png.Encode(w, result.Image)
	default:
		jpeg.Encode(w, result.Image, &jpeg.Options{Quality: jpeg.DefaultQuality})
	}
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
	s := SpectreServer{
		Server: &http.Server{
			Addr:    *listen,
			Handler: nil, // use `http.DefaultServeMux`
		},
		DB:      db,
		Samples: samples,
	}
	http.HandleFunc(collectEndpoint, s.collectHandler)
	http.HandleFunc(renderEndpoint, s.renderHandler)
	if *certFile != "" || *keyFile != "" {
		glog.Fatal(s.Server.ListenAndServeTLS(*certFile, *keyFile))
	} else {
		glog.Infoln("Resorting to serving HTTP because there was no certificate and key defined.")
		glog.Fatal(s.Server.ListenAndServe())
	}

	glog.Flush()
}
