package export

import (
	"context"
	"fmt"

	"cloud.google.com/go/datastore"
	"github.com/golang/glog"

	"github.com/hb9tf/spectre/sdr"
)

const (
	datastoreSampleCountInfo = 1000
)

type DataStore struct {
	Client *datastore.Client
}

func (d *DataStore) Write(ctx context.Context, samples <-chan sdr.Sample) error {
	counts := map[string]int{
		"error":   0,
		"success": 0,
		"total":   0,
	}
	for s := range samples {
		counts["total"] += 1
		k := datastore.IncompleteKey("Sample", nil)
		_, err := d.Client.Put(ctx, k, &s)
		if err != nil {
			counts["error"] += 1
			glog.Warningf("error storing in datastore: %s\n", err)
			continue
		}
		counts["success"] += 1
		if counts["total"]%datastoreSampleCountInfo == 0 {
			fmt.Printf("Sample export counts: %+v\n", counts)
		}
	}
	return nil
}
