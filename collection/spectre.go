package main

import (
	"context"
	"database/sql"
	"flag"
	"io/ioutil"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang/glog"
	"github.com/google/uuid"

	"github.com/hb9tf/spectre/collection/hackrf"
	"github.com/hb9tf/spectre/collection/rtlsdr"
	"github.com/hb9tf/spectre/export"
	"github.com/hb9tf/spectre/sdr"

	// Blind import support for sqlite3 used by sqlite.go.
	_ "github.com/mattn/go-sqlite3"
)

// Flags
var (
	identifier          = flag.String("identifier", "", "unique identifier of source instance (defaults to a random UUID)")
	lowFreq             = flag.Int("lowFreq", 400000000, "lower frequency boundary in Hz")
	highFreq            = flag.Int("highFreq", 450000000, "upper frequency boundary in Hz")
	binSize             = flag.Int("binSize", 12500, "size of the bin in Hz")
	integrationInterval = flag.Duration("integrationInterval", 5*time.Second, "duration to aggregate samples")
	sdrType             = flag.String("sdr", "", "SDR to use (one of: hackrf, rtlsdr)")
	output              = flag.String("output", "", "Export mechanism to use (one of: csv, sqlite, mysql, spectre)")

	// SQLite
	sqliteFile = flag.String("sqliteFile", "/tmp/spectre", "File path of the sqlite DB file to use.")

	// MySQL
	mysqlServer       = flag.String("mysqlServer", "127.0.0.1:3306", "MySQL TCP server endpoint to connect to (IP/DNS and port).")
	mysqlUser         = flag.String("mysqlUser", "", "MySQL DB user.")
	mysqlPasswordFile = flag.String("mysqlPasswordFile", "", "Path to the file containing the password for the MySQL user.")
	mysqlDBName       = flag.String("mysqlDBName", "spectre", "Name of the DB to use.")

	// Spectre Server
	spectreServer        = flag.String("spectreServer", "http://localhost:8080", "URL scheme, address and port of the spectre server.")
	spectreServerSamples = flag.Int("spectreServerSamples", 0, "Defines how many samples should be sent to the server at once.")
)

func main() {
	ctx := context.Background()
	// Set defaults for glog flags. Can be overridden via cmdline.
	flag.Set("logtostderr", "false")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "1")
	// Parse flags globally.
	flag.Parse()

	if *identifier == "" {
		*identifier = uuid.NewString()
	}

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
		glog.Exitf("%q is not a supported SDR type, pick one of: hackrf, rtlsdr", *sdrType)
	}
	opts := &sdr.Options{
		LowFreq:             *lowFreq,
		HighFreq:            *highFreq,
		BinSize:             *binSize,
		IntegrationInterval: *integrationInterval,
	}

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
		exporter = &export.SQL{
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
		exporter = &export.SQL{
			DB: db,
		}
	case "spectre":
		exporter = &export.SpectreServer{
			Server:            *spectreServer,
			SendSamplesAmount: *spectreServerSamples,
		}
	default:
		glog.Exitf("%q is not a supported export method, pick one of: csv, sqlite, mysql, spectre", *output)
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
