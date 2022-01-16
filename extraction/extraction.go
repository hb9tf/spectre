package extraction

import (
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
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
	timeFmt        = "2006-01-02T15:04:05"
	gridMarginTop  = 20  // pixels
	gridMarginLeft = 150 // pixels
	gridTickLen    = 10  // pixel
	gridMinStepX   = 100 // pixels
	gridMinStepY   = 20  // pixels
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
)

func GetMaxImageHeight(db *sql.DB, source string, startFreq, endFreq int64, startTime, endTime time.Time) (int, error) {
	statement, err := db.Prepare(getTimeResolutionTmpl)
	if err != nil {
		return 0, err
	}
	var count int
	return count, statement.QueryRow(source, startFreq, endFreq, startTime.UnixMilli(), endTime.UnixMilli(), source, startTime.UnixMilli(), endTime.UnixMilli()).Scan(&count)
}

func GetMaxImageWidth(db *sql.DB, source string, startFreq, endFreq int64, startTime, endTime time.Time) (int, error) {
	statement, err := db.Prepare(getFreqResolutionTmpl)
	if err != nil {
		return 0, err
	}
	var count int
	return count, statement.QueryRow(source, startFreq, endFreq, startTime.UnixMilli(), endTime.UnixMilli()).Scan(&count)
}

// GetColor determines the color of a pixel based on a color gradient and a pixel "level".
// http://www.andrewnoske.com/wiki/Code_-_heatmaps_and_color_gradients
// This is mostly a copy of https://github.com/finfinack/netmap/blob/master/netmap.go.
func GetColor(lvl uint16) color.RGBA {
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

func GetReadableFreq(freq int64) string {
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

func DrawGrid(source *image.RGBA, lowFreq, highFreq int64, startTime, endTime time.Time) *image.RGBA {
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
		d.DrawString(GetReadableFreq(freq))
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
