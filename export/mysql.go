package export

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/sdr"
)

const (
	mysqlSampleCountInfo = 1000

	mysqlCreateTableTmpl = `CREATE TABLE IF NOT EXISTS spectre (
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
	mysqlInsertSampleTmpl = `INSERT INTO spectre(
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

type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) Write(ctx context.Context, samples <-chan sdr.Sample) error {
	if err := mysqlCreateTableIfNotExists(m.DB); err != nil {
		return fmt.Errorf("unable to create table: %s", err)
	}

	counts := map[string]int{
		"error":   0,
		"success": 0,
		"total":   0,
	}
	for sample := range samples {
		counts["total"] += 1
		if err := mysqlInsertSample(m.DB, sample); err != nil {
			counts["error"] += 1
			glog.Warningf("error storing in sqlite DB: %s\n", err)
			continue
		}
		counts["success"] += 1
		if counts["total"]%mysqlSampleCountInfo == 0 {
			glog.Infof("Sample export counts: %+v\n", counts)
		}
	}

	return nil
}

func mysqlCreateTableIfNotExists(db *sql.DB) error {
	statement, err := db.Prepare(mysqlCreateTableTmpl)
	if err != nil {
		return err
	}
	if _, err := statement.Exec(); err != nil {
		return err
	}

	return nil
}

func mysqlInsertSample(db *sql.DB, s sdr.Sample) error {
	statement, err := db.Prepare(mysqlInsertSampleTmpl)
	if err != nil {
		return err
	}
	if _, err := statement.Exec(s.Identifier, s.Source, s.FreqCenter, s.FreqLow, s.FreqHigh, s.DBHigh, s.DBLow, s.DBAvg, s.SampleCount, s.Start.UnixMilli(), s.End.UnixMilli()); err != nil {
		return err
	}

	return nil
}
