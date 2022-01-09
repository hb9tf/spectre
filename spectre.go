package main

import (
	"context"
	"flag"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/export"
	"github.com/hb9tf/spectre/hackrf"
	"github.com/hb9tf/spectre/rtlsdr"
	"github.com/hb9tf/spectre/sdr"

	// Blind import support for sqlite3 used by sqlite.go.
	_ "github.com/mattn/go-sqlite3"
)

// Flags
var (
	identifier          = flag.String("id", "", "unique identifier of source instance (needs to be assigned!)")
	lowFreq             = flag.Int("lowFreq", 400000000, "lower frequency boundary in Hz")
	highFreq            = flag.Int("highFreq", 450000000, "upper frequency boundary in Hz")
	binSize             = flag.Int("binSize", 12500, "size of the bin in Hz")
	sampleSize          = flag.Int("samples", 8192, "samples to take per bin")
	integrationInterval = flag.Duration("integrationInterval", 5*time.Second, "duration to aggregate samples")
	sdrType             = flag.String("sdr", "", "SDR to use (one of: hackrf, rtlsdr)")
	output              = flag.String("output", "", "Export mechanism to use (one of: csv, sqlite)")

	// SQLite
	sqliteFile = flag.String("sqliteFile", "/tmp/spectre", "File path of the sqlite DB file to use.")
)

func main() {
	ctx := context.Background()
	// Set defaults for glog flags. Can be overridden via cmdline.
	flag.Set("logtostderr", "false")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "1")
	// Parse flags globally.
	flag.Parse()

	// SDR setup
	var radio sdr.SDR
	switch strings.ToLower(*sdrType) {
	case hackrf.SourceName:
		radio = &hackrf.SDR{
			Identifier: *identifier,
		}
	case rtlsdr.SourceName:
		radio = &rtlsdr.SDR{
			Identifier: *identifier,
		}
	default:
		glog.Fatalf("%q is not a supported SDR type, pick one of: hackrf, rtlsdr", *sdrType)
	}
	opts := &sdr.Options{
		LowFreq:             *lowFreq,
		HighFreq:            *highFreq,
		BinSize:             *binSize,
		SampleSize:          *sampleSize,
		IntegrationInterval: *integrationInterval,
	}

	// Exporter setup
	var exporter export.Exporter
	switch strings.ToLower(*output) {
	case "csv":
		exporter = &export.CSV{}
	case "sqlite":
		exporter = &export.SQLite{
			DBFile: *sqliteFile,
		}
	default:
		glog.Fatalf("%q is not a supported export method, pick one of: csv, sqlite", *output)
	}

	// Run
	samples := make(chan sdr.Sample)
	go func() {
		if err := radio.Sweep(opts, samples); err != nil {
			glog.Fatal(err)
		}
	}()

	if err := exporter.Write(ctx, samples); err != nil {
		glog.Fatal(err)
	}

	glog.Flush()
}
