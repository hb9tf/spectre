package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hb9tf/spectre/hackrf"
	"github.com/hb9tf/spectre/rtlsdr"
	"github.com/hb9tf/spectre/sdr"
)

// Flags
var (
	lowFreq             = flag.Int("lowFreq", 400000000, "lower frequency boundary in Hz")
	highFreq            = flag.Int("highFreq", 450000000, "upper frequency boundary in Hz")
	binSize             = flag.Int("binSize", 12500, "size of the bin in Hz")
	sampleSize          = flag.Int("samples", 8192, "samples to take per bin")
	integrationInterval = flag.Duration("integrationInterval", 5*time.Second, "duration to aggregate samples")
	sdrType             = flag.String("sdr", "", "SDR to use (one of: hackrf, rtlsdr)")
)

func main() {
	flag.Parse()

	var radio sdr.SDR
	switch strings.ToLower(*sdrType) {
	case "hackrf":
		radio = &hackrf.SDR{}
	case "rtlsdr":
		radio = &rtlsdr.SDR{}
	default:
		log.Panicf("%q is not a supported SDR type, pick one of: hackrf, rtlsdr", *sdrType)
	}

	opts := &sdr.Options{
		LowFreq:             *lowFreq,
		HighFreq:            *highFreq,
		BinSize:             *binSize,
		SampleSize:          *sampleSize,
		IntegrationInterval: *integrationInterval,
	}

	samples := make(chan sdr.Sample)
	go func() {
		if err := radio.Sweep(opts, samples); err != nil {
			log.Panicln(err)
		}
	}()

	// Output aggregated samples in regular ticks.
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{
		"FreqCenter",
		"FreqLow",
		"FreqHigh",
		"StartUnixMilli",
		"EndUnixMilli",
		"dBLow",
		"dBHigh",
		"dbAvg",
		"SampleCount",
	})

	sampleCount := 0
	for s := range samples {
		sampleCount += 1
		if err := w.Write([]string{
			fmt.Sprintf("%d", s.FreqCenter),
			fmt.Sprintf("%d", s.FreqLow),
			fmt.Sprintf("%d", s.FreqHigh),
			fmt.Sprintf("%d", s.Start.UnixMilli()),
			fmt.Sprintf("%d", s.End.UnixMilli()),
			fmt.Sprintf("%f", s.DBLow),
			fmt.Sprintf("%f", s.DBHigh),
			fmt.Sprintf("%f", s.DBAvg),
			fmt.Sprintf("%d", s.SampleCount),
		}); err != nil {
			log.Println(err)
		}

		w.Flush()
		if err := w.Error(); err != nil {
			log.Println(err)
		}
	}
}
