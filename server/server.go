package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/export"
	"github.com/hb9tf/spectre/sdr"
)

var (
	listen   = flag.String("listen", ":8443", "")
	certFile = flag.String("certFile", "", "Path of the file containing the certificate (including the chained intermediates and root) for the TLS connection.")
	keyFile  = flag.String("keyFile", "", "Path of the file containing the key for the TLS connection.")
	output   = flag.String("output", "", "Export mechanism to use (one of: csv, sqlite)")

	// SQLite
	sqliteFile = flag.String("sqliteFile", "/tmp/spectre", "File path of the sqlite DB file to use.")
)

const (
	collectEndpoint = "/spectre/v1/collect"
)

type SpectreServer struct {
	server  *http.Server
	samples chan sdr.Sample
}

func (s *SpectreServer) collectHandler(w http.ResponseWriter, r *http.Request) {
	samples := []sdr.Sample{}
	if err := json.NewDecoder(r.Body).Decode(&samples); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, sample := range samples {
		s.samples <- sample
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
		glog.Exitf("%q is not a supported export method, pick one of: csv, sqlite", *output)
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
		server: &http.Server{
			Addr:    *listen,
			Handler: nil, // use `http.DefaultServeMux`
		},
		samples: samples,
	}
	http.HandleFunc(collectEndpoint, s.collectHandler)
	if *certFile != "" || *keyFile != "" {
		glog.Fatal(s.server.ListenAndServeTLS(*certFile, *keyFile))
	} else {
		glog.Infoln("Resorting to serving HTTP because there was no certificate and key defined.")
		glog.Fatal(s.server.ListenAndServe())
	}

	glog.Flush()
}
