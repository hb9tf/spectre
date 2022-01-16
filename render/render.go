package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io/ioutil"
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
	sdr          = flag.String("sdr", "rtlsdr", "Source type, e.g. rtlsdr or hackrf.")
	startFreq    = flag.Int64("startFreq", 0, "Select samples starting with this frequency in Hz.")
	endFreq      = flag.Int64("endFreq", math.MaxInt64, "Select samples up to this frequency in Hz.")
	startTimeRaw = flag.String("startTime", "2000-01-02T15:04:05", "Select samples collected after this time. Format: 2006-01-02T15:04:05")
	endTimeRaw   = flag.String("endTime", "2100-01-02T15:04:05", "Select samples collected before this time. Format: 2006-01-02T15:04:05")

	// Image rendering options
	addGrid   = flag.Bool("addGrid", true, "Adds a grid to the output image for reference when set.")
	imgPath   = flag.String("imgPath", "/tmp/out.jpg", "Path where the rendered image should be written to.")
	imgWidth  = flag.Int("imgWidth", 0, "Width of output image in pixels.")
	imgHeight = flag.Int("imgHeight", 0, "Height of output image in pixels.")
)

const (
	timeFmt        = "2006-01-02T15:04:05"
	getImgDataTmpl = `SELECT
		MIN(FreqLow),
		AVG(FreqCenter),
		MAX(FreqHigh),
		MAX(DBHigh),
		MIN(Start),
		MAX(End),
		TimeBucket,
		FreqBucket
	FROM (
		SELECT
			FreqLow,
			FreqCenter,
			FreqHigh,
			DBHigh,
			Start,
			End,
			NTILE (?) OVER (ORDER BY Start) TimeBucket,
			NTILE (?) OVER (ORDER BY FreqCenter) FreqBucket
		FROM
			spectre
		WHERE
			Source = ?
			AND FreqLow >= ?
			AND FreqHigh <= ?
			AND Start >= ?
			AND End <= ?
		ORDER BY
			TimeBucket ASC,
			FreqBucket ASC
	)
	GROUP BY TimeBucket, FreqBucket;`
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
	default:
		glog.Exitf("%q is not a supported source, pick one of: sqlite", *source)
	}

	maxImgHeight, err := extraction.GetMaxImageHeight(db, *sdr, *startFreq, *endFreq, startTime, endTime)
	if err != nil {
		glog.Exitf("unable to query sqlite DB to determine image height: %s\n", err)
	}
	switch {
	case *imgHeight == 0:
		*imgHeight = maxImgHeight
	case *imgHeight > 0 && *imgHeight > maxImgHeight:
		glog.Warningf("-imgHeight is set to %d which is more than what the data in the sqlite DB can provide. Reducing image height to %d pixels\n", *imgHeight, maxImgHeight)
		*imgHeight = maxImgHeight
	}
	maxImgWidth, err := extraction.GetMaxImageWidth(db, *sdr, *startFreq, *endFreq, startTime, endTime)
	if err != nil {
		glog.Exitf("unable to query sqlite DB to determine image width: %s\n", err)
	}
	switch {
	case *imgWidth == 0:
		*imgWidth = maxImgWidth
	case *imgWidth > 0 && *imgWidth > maxImgWidth:
		glog.Warningf("-imgWidth is set to %d which is more than what the data in the sqlite DB can provide. Reducing image width to %d pixels\n", *imgWidth, maxImgWidth)
		*imgWidth = maxImgWidth
	}

	statement, err := db.Prepare(getImgDataTmpl)
	if err != nil {
		glog.Exit(err)
	}
	imgData, err := statement.Query(*imgHeight, *imgWidth, *sdr, *startFreq, *endFreq, startTime.UnixMilli(), endTime.UnixMilli())
	if err != nil {
		glog.Fatal(err)
	}

	lowFreq := int64(math.MaxInt64)
	highFreq := int64(0)
	globalMinDB := float32(1000)  // assuming no dB value will be higher than this so it constantly gets corrected downwards
	globalMaxDB := float32(-1000) // assuming no dB value will be lower than this so it constantly gets corrected upwards
	sTime := time.Now()
	var eTime time.Time

	img := map[int]map[int]float32{}
	for imgData.Next() {
		var freqLow, freqHigh int64
		var timeStart, timeEnd int64
		var freqCenter float64
		var db float32
		var rowIdx, colIdx int
		if err := imgData.Scan(&freqLow, &freqCenter, &freqHigh, &db, &timeStart, &timeEnd, &rowIdx, &colIdx); err != nil {
			glog.Warningf("unable to get sample from DB: %s\n", err)
			continue
		}

		start := time.Unix(0, timeStart*int64(time.Millisecond))
		if start.Before(sTime) {
			sTime = start
		}
		end := time.Unix(0, timeEnd*int64(time.Millisecond))
		if end.After(eTime) {
			eTime = end
		}

		if db < globalMinDB {
			globalMinDB = db
		}
		if db > globalMaxDB {
			globalMaxDB = db
		}
		if freqLow < lowFreq {
			lowFreq = freqLow
		}
		if freqHigh > highFreq {
			highFreq = freqHigh
		}

		if _, ok := img[rowIdx]; !ok {
			img[rowIdx] = map[int]float32{}
		}
		img[rowIdx][colIdx] = db
	}
	imgData.Close()

	fmt.Println("Selected source metadata:")
	fmt.Printf("  - Low frequency: %s\n", extraction.GetReadableFreq(lowFreq))
	fmt.Printf("  - High frequency: %s\n", extraction.GetReadableFreq(highFreq))
	fmt.Printf("  - Start time: %s (%d)\n", sTime.Format(timeFmt), sTime.Unix())
	fmt.Printf("  - End time: %s (%d)\n", eTime.Format(timeFmt), eTime.Unix())
	fmt.Printf("  - Duration: %s\n", eTime.Sub(sTime))
	fmt.Printf("Rendering image (%d x %d)\n", *imgWidth, *imgHeight)
	fmt.Printf("  - Frequency resolution: %s per pixel\n", extraction.GetReadableFreq(int64(float64(highFreq-lowFreq)/float64(*imgWidth))))
	fmt.Printf("  - Time resultion: %.2f seconds per pixel\n", eTime.Sub(sTime).Seconds()/float64(*imgHeight))

	// Create image canvas.
	canvas := image.NewRGBA(image.Rectangle{
		Min: image.Point{0, 0},
		Max: image.Point{*imgWidth, *imgHeight},
	})

	// Draw waterfall.
	dbRange := globalMaxDB - globalMinDB
	minlvl := uint16(math.MaxUint16)
	maxlvl := uint16(0)
	for rowIdx, row := range img {
		for columnIdx, db := range row {
			lvl := uint16((db - globalMinDB) * math.MaxUint16 / dbRange)
			if lvl < minlvl {
				minlvl = lvl
			}
			if lvl > maxlvl {
				maxlvl = lvl
			}
			canvas.SetRGBA(columnIdx, rowIdx, extraction.GetColor(lvl))
		}
	}

	// Draw grid.
	if *addGrid {
		canvas = extraction.DrawGrid(canvas, lowFreq, highFreq, sTime, eTime)
	}

	fmt.Printf("Writing image to %q\n", *imgPath)
	f, _ := os.Create(*imgPath)
	defer f.Close()
	switch {
	case strings.HasSuffix(*imgPath, ".png"):
		png.Encode(f, canvas)
	case strings.HasSuffix(*imgPath, ".jpg"):
		jpeg.Encode(f, canvas, &jpeg.Options{Quality: jpeg.DefaultQuality})
	}
}
