package main

/*
This application can be used to render waterfalls for data
collected with Spectre.

It currently only supports data collected into sqlite.

Note: This is HIGHLY experimental. You've been warned.
*/

import (
	"database/sql"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"

	"github.com/golang/glog"

	// Blind import support for sqlite3 used by sqlite.go.
	_ "github.com/mattn/go-sqlite3"
)

// Flags
var (
	sqliteFile = flag.String("sqliteFile", "/tmp/spectre", "File path of the sqlite DB file to use.")
	source     = flag.String("source", "rtl_sdr", "Source type, e.g. rtl_sdr or hackrf.")
	imgPath    = flag.String("imgPath", "/tmp/out.jpg", "Path where the rendered image should be written to.")
	imgWidth   = flag.Int("imgWidth", 640, "Width of output image in pixels.")
	imgHeight  = flag.Int("imgHeight", 480, "Height of output image in pixels.")
)

var (
	// Colors defining the gradient in the heatmap. The higher the index, the warmer.
	colors = map[int]color.RGBA{
		0: color.RGBA{0, 0, 0, 255},       // black
		1: color.RGBA{0, 0, 255, 255},     // blue
		2: color.RGBA{0, 255, 255, 255},   // cyan
		3: color.RGBA{0, 255, 0, 255},     // green
		4: color.RGBA{255, 255, 0, 255},   // yellow
		5: color.RGBA{255, 0, 0, 255},     // red
		6: color.RGBA{255, 255, 255, 255}, // white
	}
)

const (
	getTimeBucketsTmpl = `SELECT
		Start,
		NTILE (?) OVER (ORDER BY Start) TimeBucket
	FROM
		spectre
	WHERE
		Source = ?
	GROUP BY
		Start
	ORDER BY
		TimeBucket ASC
	;`
	getFreqBucketsTmpl = `SELECT
		FreqCenter,
		MAX(DBAvg),
		NTILE (?) OVER (ORDER BY FreqCenter) FreqBucket
	FROM
		spectre
	WHERE
		Source = ?
		AND Start IN (%s)
	GROUP BY
		FreqCenter
	ORDER BY
		FreqBucket,
		FreqCenter ASC
	;`
)

// getColor determines the color of a pixel based on a color gradient and a pixel "level".
// http://www.andrewnoske.com/wiki/Code_-_heatmaps_and_color_gradients
func getColor(lvl uint16) color.RGBA {
	// Find the first color in the gradient where the "level" is higher than the level we're looking for.
	// Then determine how far along we are between the previous and next color in the gradient and use that
	// to calculate the color between the two.
	for i := 0; i < len(colors); i++ {
		currC := colors[i]
		currV := uint16(i * math.MaxUint16 / len(colors))
		if lvl < currV {
			prevC := colors[int(math.Max(0.0, float64(i-1)))]
			diff := uint16(math.Max(0.0, float64(i-1)))*math.MaxUint16/uint16(len(colors)) - currV
			fract := 0.0
			if diff != 0 {
				fract = float64(lvl) - float64(currV)/float64(diff)
			}
			return color.RGBA{
				uint8(float64(prevC.R-currC.R)*fract + float64(currC.R)),
				uint8(float64(prevC.G-currC.G)*fract + float64(currC.G)),
				uint8(float64(prevC.B-currC.B)*fract + float64(currC.B)),
				uint8(float64(prevC.A-currC.A)*fract + float64(currC.A)),
			}
		}
	}
	return colors[len(colors)-1]
}

func main() {
	// Set defaults for glog flags. Can be overridden via cmdline.
	flag.Set("logtostderr", "false")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "1")
	// Parse flags globally.
	flag.Parse()

	db, err := sql.Open("sqlite3", *sqliteFile)
	if err != nil {
		glog.Fatalf("unable to open sqlite DB %q: %s", sqliteFile, err)
	}

	timeStmt, err := db.Prepare(getTimeBucketsTmpl)
	if err != nil {
		glog.Fatal(err)
	}
	timeBuckets, err := timeStmt.Query(*imgHeight, *source)
	if err != nil {
		glog.Fatal(err)
	}

	rows := map[int][]int64{}
	for timeBuckets.Next() {
		var start int64
		var rowIdx int
		if err := timeBuckets.Scan(&start, &rowIdx); err != nil {
			glog.Warningf("unable to get sample from DB: %s\n", err)
			continue
		}
		if _, ok := rows[rowIdx]; !ok {
			rows[rowIdx] = []int64{}
		}
		rows[rowIdx] = append(rows[rowIdx], start)
	}
	timeBuckets.Close()

	img := map[int]map[int]float32{}
	for rowIdx := 1; rowIdx <= len(rows); rowIdx++ {
		fmt.Printf("Collecting data for image row %d (of %d)\n", rowIdx, len(rows))

		startTimes, ok := rows[rowIdx]
		if !ok {
			glog.Warningf("unable to fetch start times for time bucket %d from index", rowIdx)
			continue
		}
		img[rowIdx] = map[int]float32{}

		startTimesIn := []string{}
		for _, s := range startTimes {
			startTimesIn = append(startTimesIn, fmt.Sprintf("%d", s))
		}
		freqDBStmt, err := db.Prepare(fmt.Sprintf(getFreqBucketsTmpl, strings.Join(startTimesIn, ", ")))
		if err != nil {
			glog.Fatal(err)
		}

		freqBuckets, err := freqDBStmt.Query(*imgWidth, *source)
		if err != nil {
			glog.Fatal(err)
		}

		for freqBuckets.Next() {
			var freqCenter int64
			var db float32
			var freqBucket int
			if err := freqBuckets.Scan(&freqCenter, &db, &freqBucket); err != nil {
				glog.Warningf("unable to get sample from DB: %s\n", err)
				continue
			}

			dbMax, ok := img[rowIdx][freqBucket]
			if !ok || db > dbMax {
				img[rowIdx][freqBucket] = db
			}
		}
		freqBuckets.Close()
	}

	globalMinDB := float32(1000)  // assuming no dB value will be higher than this so it constantly gets corrected downwards
	globalMaxDB := float32(-1000) // assuming no dB value will be lower than this so it constantly gets corrected upwards
	for _, row := range img {
		for _, db := range row {
			if db < globalMinDB {
				globalMinDB = db
			}
			if db > globalMaxDB {
				globalMaxDB = db
			}
		}
	}

	fmt.Printf("Rendering image (%d x %d)\n", *imgWidth, *imgHeight)
	canvas := image.NewRGBA(image.Rectangle{
		Min: image.Point{0, 0},
		Max: image.Point{*imgWidth, *imgHeight},
	})
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
			canvas.SetRGBA(columnIdx, rowIdx, getColor(lvl))
		}
	}

	fmt.Printf("Writing image to %q\n", *imgPath)
	f, _ := os.Create(*imgPath)
	defer f.Close()
	switch {
	case strings.HasSuffix(*imgPath, ".png"):
		png.Encode(f, canvas)
	case strings.HasSuffix(*imgPath, ".jpg"):
		jpeg.Encode(f, canvas, &jpeg.Options{jpeg.DefaultQuality})
	}
}
