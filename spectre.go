package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/datastore"
	"google.golang.org/api/option"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/hb9tf/spectre/export"
	"github.com/hb9tf/spectre/hackrf"
	"github.com/hb9tf/spectre/rtlsdr"
	"github.com/hb9tf/spectre/sdr"
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
	output              = flag.String("output", "", "Export mechanism to use (one of: csv, elastic, datastore)")

	// Elastic
	esEndpoints = flag.String("esEndpoints", "http://localhost:9200", "Comma separated list of endpoints for elastic export.")
	esUser      = flag.String("esUser", "elastic", "Username to use for elastic export.")
	esPwdFile   = flag.String("esPwdFile", "", "File to read password for elastic export from.")

	// GCP
	gcpProject           = flag.String("gcpProject", "", "GCP project")
	gcpServiceAccountKey = flag.String("gcpSvcAcctKey", "", "GCP Service accout key file (JSON)")
)

func main() {
	ctx := context.Background()
	flag.Parse()

	// SDR setup
	var radio sdr.SDR
	switch strings.ToLower(*sdrType) {
	case "hackrf":
		radio = &hackrf.SDR{
			Identifier: *identifier,
		}
	case "rtlsdr":
		radio = &rtlsdr.SDR{
			Identifier: *identifier,
		}
	default:
		log.Fatalf("%q is not a supported SDR type, pick one of: hackrf, rtlsdr", *sdrType)
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
	case "elastic":
		pwd, err := os.ReadFile(*esPwdFile)
		if err != nil {
			log.Fatalf("unable to read password file %q for Elastic export: %s", *esPwdFile, err)
		}
		cfg := elasticsearch.Config{
			Addresses: strings.Split(*esEndpoints, ","),
			Username:  *esUser,
			Password:  strings.TrimSpace(string(pwd)),
		}
		esClient, err := elasticsearch.NewClient(cfg)
		if err != nil {
			log.Fatalf("failed to create elastic client: %s", err)
		}
		exporter = &export.Elastic{
			Client: esClient,
		}
	case "datastore":
		dsClient, err := datastore.NewClient(ctx, *gcpProject, option.WithCredentialsFile(*gcpServiceAccountKey))
		if err != nil {
			log.Fatalf("failed to create datastore client: %s", err)
		}
		defer dsClient.Close()
		exporter = &export.DataStore{
			Client: dsClient,
		}
	default:
		log.Fatalf("%q is not a supported export method, pick one of: csv, datastore", *output)
	}

	// Run
	samples := make(chan sdr.Sample)
	go func() {
		if err := radio.Sweep(opts, samples); err != nil {
			log.Fatal(err)
		}
	}()

	if err := exporter.Write(ctx, samples); err != nil {
		log.Fatal(err)
	}
}
