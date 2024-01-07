package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang/glog"

	"github.com/hb9tf/spectre/extraction"

	// Blind import support for sqlite3 used by sqlite.go.
	_ "github.com/mattn/go-sqlite3"
)

// Flags
var (
	source = flag.String("source", "sqlite", "Source type, e.g. sqlite or mysql.")
	// SQLite
	sqliteFile = flag.String("sqliteFile", "/tmp/spectre", "File path of the sqlite DB file to use.")

	// MySQL
	mysqlServer       = flag.String("mysqlServer", "127.0.0.1:3306", "MySQL TCP server endpoint to connect to (IP/DNS and port).")
	mysqlUser         = flag.String("mysqlUser", "", "MySQL DB user.")
	mysqlPasswordFile = flag.String("mysqlPasswordFile", "", "Path to the file containing the password for the MySQL user.")
	mysqlDBName       = flag.String("mysqlDBName", "spectre", "Name of the DB to use.")

	// Filter options
	sdr          = flag.String("sdr", "", "Source type, e.g. rtlsdr or hackrf.")
	identifier   = flag.String("identifier", "", "Identifier of the station to render the data for (typically a UUID4).")
	startFreq    = flag.Uint("startFreq", 0, "Select samples starting with this frequency in Hz.")
	endFreq      = flag.Uint("endFreq", math.MaxUint, "Select samples up to this frequency in Hz.")
	startTimeRaw = flag.String("startTime", "1970-01-01T00:00:00", "Select samples collected after this time. Format: 2006-01-02T15:04:05")
	endTimeRaw   = flag.String("endTime", "2100-01-02T15:04:05", "Select samples collected before this time. Format: 2006-01-02T15:04:05")

	// Image rendering options
	addGrid   = flag.Bool("addGrid", true, "Adds a grid to the output image for reference when set.")
	imgPath   = flag.String("imgPath", "/tmp/out.jpg", "Path where the rendered image should be written to.")
	imgWidth  = flag.Int("imgWidth", 0, "Width of output image in pixels.")
	imgHeight = flag.Int("imgHeight", 0, "Height of output image in pixels.")
)

const (
	timeFmt = "2006-01-02T15:04:05"
)

func main() {
	// Set defaults for glog flags. Can be overridden via cmdline.
	flag.Set("logtostderr", "false")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "1")
	// Parse flags globally.
	flag.Parse()

	startTime, err := time.Parse(timeFmt, *startTimeRaw)
	if err != nil {
		glog.Exitf("unable to parse startTime (value: %q, format: %q): %s", *startTimeRaw, timeFmt, err)
	}
	endTime, err := time.Parse(timeFmt, *endTimeRaw)
	if err != nil {
		glog.Exitf("unable to parse endTime (value: %q, format: %q): %s", *endTimeRaw, timeFmt, err)
	}

	var db *sql.DB
	switch strings.ToLower(*source) {
	case "sqlite":
		if _, err := os.Stat(*sqliteFile); errors.Is(err, os.ErrNotExist) {
			glog.Exitf("unable to open sqlite DB %q: %s", sqliteFile, err)
		}
		var err error
		db, err = sql.Open("sqlite3", *sqliteFile)
		if err != nil {
			glog.Exitf("unable to open sqlite DB %q: %s", *sqliteFile, err)
		}
	case "mysql":
		pass, err := os.ReadFile(*mysqlPasswordFile)
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
	default:
		glog.Exitf("%q is not a supported source, pick one of: sqlite", *source)
	}

	result, err := extraction.Render(db, &extraction.RenderRequest{
		Image: &extraction.ImageOptions{
			Height:  *imgHeight,
			Width:   *imgWidth,
			AddGrid: *addGrid,
		},
		Filter: &extraction.FilterOptions{
			SDR:        *sdr,
			Identifier: *identifier,
			StartFreq:  *startFreq,
			EndFreq:    *endFreq,
			StartTime:  startTime,
			EndTime:    endTime,
		},
	})
	if err != nil {
		glog.Exitf("Unable to render image: %s\n", err)
	}

	fmt.Println("Selected source metadata:")
	fmt.Printf("  - Low frequency: %s\n", extraction.GetReadableFreq(result.SourceMeta.LowFreq))
	fmt.Printf("  - High frequency: %s\n", extraction.GetReadableFreq(result.SourceMeta.HighFreq))
	fmt.Printf("  - Start time: %s (%d)\n", result.SourceMeta.StartTime.Format(timeFmt), result.SourceMeta.StartTime.Unix())
	fmt.Printf("  - End time: %s (%d)\n", result.SourceMeta.EndTime.Format(timeFmt), result.SourceMeta.EndTime.Unix())
	fmt.Printf("  - Duration: %s\n", result.SourceMeta.EndTime.Sub(result.SourceMeta.StartTime))
	fmt.Printf("Rendered image (%d x %d)\n", result.ImageMeta.ImageWidth, result.ImageMeta.ImageHeight)
	fmt.Printf("  - Frequency resolution: %s per pixel\n", extraction.GetReadableFreq(uint(result.ImageMeta.FreqPerPixel)))
	fmt.Printf("  - Time resolution: %.2f seconds per pixel\n", result.ImageMeta.SecPerPixel)

	fmt.Printf("Writing image to %q\n", *imgPath)
	f, _ := os.Create(*imgPath)
	defer f.Close()
	switch {
	case strings.HasSuffix(*imgPath, ".png"):
		png.Encode(f, result.Image)
	case strings.HasSuffix(*imgPath, ".jpg"):
		jpeg.Encode(f, result.Image, &jpeg.Options{Quality: jpeg.DefaultQuality})
	}
}
