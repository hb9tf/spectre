package export

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/collection/sdr"
)

const (
	sqliteSampleCountInfo = 1000

	createTableTmpl = `CREATE TABLE IF NOT EXISTS spectre (
		"ID"           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"Identifier"   TEXT NOT NULL,
		"Source"       TEXT NOT NULL,
		"FreqCenter"   INTEGER,
		"FreqLow"      INTEGER,
		"FreqHigh"     INTEGER,
		"DBHigh"       REAL,
		"DBLow"        REAL,
		"DBAvg"        REAL,
		"SampleCount"  INTEGER,
		"Start"        INTEGER,
		"End"          INTEGER
	);`
	insertSampleTmpl = `INSERT INTO spectre(
		Identifier,
		Source,
		FreqCenter,
		FreqLow,
		FreqHigh,
		DBHigh,
		DBLow,
		DBAvg,
		SampleCount,
		Start,
		End
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
)

type SQLite struct {
	DBFile string
}

func (s *SQLite) Write(ctx context.Context, samples <-chan sdr.Sample) error {
	db, err := sql.Open("sqlite3", s.DBFile)
	if err != nil {
		return fmt.Errorf("unable to open sqlite DB %q: %s", s.DBFile, err)
	}

	if err := createTableIfNotExists(db); err != nil {
		return fmt.Errorf("unable to create table: %s", err)
	}

	counts := map[string]int{
		"error":   0,
		"success": 0,
		"total":   0,
	}
	for s := range samples {
		counts["total"] += 1
		if err := insertSample(db, s); err != nil {
			counts["error"] += 1
			glog.Warningf("error storing in sqlite DB: %s\n", err)
			continue
		}
		counts["success"] += 1
		if counts["total"]%sqliteSampleCountInfo == 0 {
			fmt.Printf("Sample export counts: %+v\n", counts)
		}
	}

	return nil
}

func createTableIfNotExists(db *sql.DB) error {
	statement, err := db.Prepare(createTableTmpl)
	if err != nil {
		return err
	}
	if _, err := statement.Exec(); err != nil {
		return err
	}

	return nil
}

func insertSample(db *sql.DB, s sdr.Sample) error {
	statement, err := db.Prepare(insertSampleTmpl)
	if err != nil {
		return err
	}
	if _, err := statement.Exec(s.Identifier, s.Source, s.FreqCenter, s.FreqLow, s.FreqHigh, s.DBHigh, s.DBLow, s.DBAvg, s.SampleCount, s.Start.UnixMilli(), s.End.UnixMilli()); err != nil {
		return err
	}

	return nil
}
