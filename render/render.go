package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang/glog"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

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

var (
	// Colors defining the gradient in the heatmap. The higher the index, the warmer.
	colors = map[int]color.RGBA{
		0: {0, 0, 0, 255},       // black
		1: {0, 0, 255, 255},     // blue
		2: {0, 255, 255, 255},   // cyan
		3: {0, 255, 0, 255},     // green
		4: {255, 255, 0, 255},   // yellow
		5: {255, 0, 0, 255},     // red
		6: {255, 255, 255, 255}, // white
	}

	gridColor           = color.RGBA{0, 0, 0, 255}       // white
	gridBackgroundColor = color.RGBA{255, 255, 255, 255} // black

	expSuffixLookup = map[int]string{
		0: "Hz",  // 10^0
		1: "kHz", // 10^3
		2: "MHz", // 10^6
		3: "GHz", // 10^9
		4: "THz", // 10^12
	}
)

const (
	gridMarginTop  = 20  // pixels
	gridMarginLeft = 150 // pixels
	gridTickLen    = 10  // pixel
	gridMinStepX   = 100 // pixels
	gridMinStepY   = 20  // pixels
	timeFmt        = "2006-01-02T15:04:05"
	// getFreqResolutionTmpl is the sqlite query to get the number of distinct frequencies
	// in the DB. This results in the maximum amount of pixels in the X axis we should render.
	// This is possible because the frequency centers remain the same across a run.
	getFreqResolutionTmpl = `SELECT
		COUNT(DISTINCT(FreqCenter))
	FROM
		spectre
	WHERE
		Source = ?
		AND FreqLow >= ?
		AND FreqHigh <= ?
		AND Start >= ?
		AND End <= ?;`
	// getTimeResolution is the sqlite query to get the number of distinct timestamps
	// for a frequency in the DB. This results in the maximum amount of pixels in the Y
	// axis we should render.
	// This is more involved because the timestamps are different per frequency.
	getTimeResolutionTmpl = `SELECT
			COUNT(DISTINCT(Start))
		FROM
			spectre AS s
		WHERE
			s.FreqCenter = (
				SELECT
					MIN(FreqCenter)
				FROM
					spectre
				WHERE
					Source = ?
					AND FreqLow >= ?
					AND FreqHigh <= ?
					AND Start >= ?
					AND End <= ?
			)
			AND Source = ?
			AND Start >= ?
			AND End <= ?;`
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

// getColor determines the color of a pixel based on a color gradient and a pixel "level".
// http://www.andrewnoske.com/wiki/Code_-_heatmaps_and_color_gradients
// This is mostly a copy of https://github.com/finfinack/netmap/blob/master/netmap.go.
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

func getReadableFreq(freq int64) string {
	exp := 0
	for f := float64(freq); f > 1000; f = f / 1000.0 {
		exp += 1
	}
	suffix, ok := expSuffixLookup[exp]
	if !ok {
		return fmt.Sprintf("%d Hz", freq)
	}
	return fmt.Sprintf("%.2f %s", float64(freq)/math.Pow(1000, float64(exp)), suffix)
}

func drawTick(canvas *image.RGBA, start image.Point, length int, horizontal bool) {
	for i := 0; i <= length; i++ {
		if horizontal {
			canvas.SetRGBA(start.X+i, start.Y, gridColor)
		} else {
			canvas.SetRGBA(start.X, start.Y+i, gridColor)
		}
	}
}

func findGridStepSize(step int, horizontal bool) int {
	gridMinStep := gridMinStepY
	if horizontal {
		gridMinStep = gridMinStepX
	}
	for step > gridMinStep {
		n := step / 2
		if n < gridMinStep {
			return step
		}
		step = n
	}
	return step
}

func drawGrid(source *image.RGBA, lowFreq, highFreq int64, startTime, endTime time.Time) *image.RGBA {
	// Enlarge existing image.
	canvas := image.NewRGBA(image.Rectangle{
		Min: image.Point{source.Bounds().Min.X, source.Bounds().Min.Y},
		Max: image.Point{source.Bounds().Max.X - 1 + gridMarginLeft, source.Bounds().Max.Y - 1 + gridMarginTop},
	})
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{gridBackgroundColor}, canvas.Bounds().Min, draw.Src)
	r := canvas.Bounds()
	r.Min.X += gridMarginLeft
	r.Min.Y += gridMarginTop
	draw.Draw(canvas, r, source, source.Bounds().Min, draw.Src)

	// Draw grid.

	// Draw X ticks.
	xStep := findGridStepSize(source.Bounds().Max.X, true)
	for i := source.Bounds().Min.X; i < source.Bounds().Max.X; i += xStep {
		// Draw the tick.
		drawTick(canvas, image.Point{
			canvas.Bounds().Min.X + gridMarginLeft + i,
			canvas.Bounds().Min.Y + gridMarginTop - gridTickLen,
		}, gridTickLen, false)
		// Label the tick.
		point := fixed.Point26_6{
			X: fixed.Int26_6((canvas.Bounds().Min.X + gridMarginLeft + i + 5) * 64),
			Y: fixed.Int26_6((canvas.Bounds().Min.Y + gridMarginTop - 2) * 64),
		}
		d := &font.Drawer{
			Dst:  canvas,
			Src:  image.NewUniform(gridColor),
			Face: basicfont.Face7x13,
			Dot:  point,
		}
		freq := lowFreq + ((int64(i) * (highFreq - lowFreq)) / int64(source.Bounds().Max.X))
		d.DrawString(getReadableFreq(freq))
	}

	// Draw Y ticks.
	yStep := findGridStepSize(source.Bounds().Max.Y, false)
	for i := source.Bounds().Min.Y; i < source.Bounds().Max.Y; i += yStep {
		// Draw the tick.
		drawTick(canvas, image.Point{
			canvas.Bounds().Min.X + gridMarginLeft - gridTickLen,
			canvas.Bounds().Min.Y + gridMarginTop + i,
		}, gridTickLen, true)
		// Label the tick.
		timePoint := fixed.Point26_6{
			X: fixed.Int26_6((canvas.Bounds().Min.X + 5) * 64),
			Y: fixed.Int26_6((canvas.Bounds().Min.Y + gridMarginTop + i + 17) * 64),
		}
		timeDrawer := &font.Drawer{
			Dst:  canvas,
			Src:  image.NewUniform(gridColor),
			Face: basicfont.Face7x13,
			Dot:  timePoint,
		}
		durPoint := fixed.Point26_6{
			X: fixed.Int26_6((canvas.Bounds().Min.X + 5) * 64),
			Y: fixed.Int26_6((canvas.Bounds().Min.Y + gridMarginTop + i + 5) * 64),
		}
		durDrawer := &font.Drawer{
			Dst:  canvas,
			Src:  image.NewUniform(gridColor),
			Face: basicfont.Face7x13,
			Dot:  durPoint,
		}
		t := (int64(i) * endTime.Sub(startTime).Milliseconds()) / int64(source.Bounds().Max.Y)
		dur, _ := time.ParseDuration(fmt.Sprintf("%dms", t))
		timeDrawer.DrawString(startTime.Add(dur).Format(timeFmt))
		durDrawer.DrawString(dur.String())
	}

	return canvas
}

func getMaxImageHeight(db *sql.DB, source string, startFreq, endFreq int64, startTime, endTime time.Time) (int, error) {
	statement, err := db.Prepare(getTimeResolutionTmpl)
	if err != nil {
		return 0, err
	}
	var count int
	return count, statement.QueryRow(source, startFreq, endFreq, startTime.UnixMilli(), endTime.UnixMilli(), source, startTime.UnixMilli(), endTime.UnixMilli()).Scan(&count)
}

func getMaxImageWidth(db *sql.DB, source string, startFreq, endFreq int64, startTime, endTime time.Time) (int, error) {
	statement, err := db.Prepare(getFreqResolutionTmpl)
	if err != nil {
		return 0, err
	}
	var count int
	return count, statement.QueryRow(source, startFreq, endFreq, startTime.UnixMilli(), endTime.UnixMilli()).Scan(&count)
}

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

	maxImgHeight, err := getMaxImageHeight(db, *sdr, *startFreq, *endFreq, startTime, endTime)
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
	maxImgWidth, err := getMaxImageWidth(db, *sdr, *startFreq, *endFreq, startTime, endTime)
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
	fmt.Printf("  - Low frequency: %s\n", getReadableFreq(lowFreq))
	fmt.Printf("  - High frequency: %s\n", getReadableFreq(highFreq))
	fmt.Printf("  - Start time: %s (%d)\n", sTime.Format(timeFmt), sTime.Unix())
	fmt.Printf("  - End time: %s (%d)\n", eTime.Format(timeFmt), eTime.Unix())
	fmt.Printf("  - Duration: %s\n", eTime.Sub(sTime))
	fmt.Printf("Rendering image (%d x %d)\n", *imgWidth, *imgHeight)
	fmt.Printf("  - Frequency resolution: %s per pixel\n", getReadableFreq(int64(float64(highFreq-lowFreq)/float64(*imgWidth))))
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
			canvas.SetRGBA(columnIdx, rowIdx, getColor(lvl))
		}
	}

	// Draw grid.
	if *addGrid {
		canvas = drawGrid(canvas, lowFreq, highFreq, sTime, eTime)
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
