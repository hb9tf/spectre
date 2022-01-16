package export

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/sdr"
)

type CSV struct{}

func (c *CSV) Write(ctx context.Context, samples <-chan sdr.Sample) error {
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{
		"Source",
		"Identifier",
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

	for s := range samples {
		if err := w.Write([]string{
			s.Source,
			s.Identifier,
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
			glog.Warningf("error while writing CSV line: %s\n", err)
		}

		w.Flush()
		if err := w.Error(); err != nil {
			glog.Warningf("error flushing CSV: %s\n", err)
		}
	}
	return nil
}
