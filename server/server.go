package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang/glog"

	"github.com/hb9tf/spectre/export"
	"github.com/hb9tf/spectre/sdr"

	// Blind import support for sqlite3 used by sqlite.go.
	_ "github.com/mattn/go-sqlite3"
)

var (
	listen   = flag.String("listen", ":8443", "")
	certFile = flag.String("certFile", "", "Path of the file containing the certificate (including the chained intermediates and root) for the TLS connection.")
	keyFile  = flag.String("keyFile", "", "Path of the file containing the key for the TLS connection.")
	output   = flag.String("output", "", "Export mechanism to use (one of: csv, sqlite)")

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
		db, err := sql.Open("sqlite3", *sqliteFile)
		if err != nil {
			glog.Exitf("unable to open sqlite DB %q: %s", *sqliteFile, err)
		}
		exporter = &export.SQLite{
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
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			glog.Exitf("unable to open MySQL DB %q: %s", *mysqlServer, err)
		}
		db.SetConnMaxLifetime(3 * time.Minute)
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(10)
		exporter = &export.MySQL{
			DB: db,
		}
	default:
		glog.Exitf("%q is not a supported export method, pick one of: csv, sqlite, mysql", *output)
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
