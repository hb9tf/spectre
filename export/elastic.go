package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"

	elasticsearch "github.com/elastic/go-elasticsearch/v7"
	esapi "github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/golang/glog"

	"github.com/hb9tf/spectre/sdr"
)

const (
	esIndexName       = "spectre"
	esSampleCountInfo = 1000
)

type Elastic struct {
	Client *elasticsearch.Client
}

func getDocID(sample sdr.Sample) string {
	return fmt.Sprintf("%s::%d::%d", sample.Identifier, sample.FreqCenter, sample.Start.UnixMilli())
}

func (e *Elastic) Write(ctx context.Context, samples <-chan sdr.Sample) error {
	// Information gathering.
	res, err := e.Client.Info()
	if err != nil {
		return err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	glog.Infof("using Elastic client version %s and connected to server: %s", elasticsearch.Version, body)
	res.Body.Close()

	// Start exporting.
	counts := map[string]int{
		"error":   0,
		"success": 0,
		"total":   0,
	}
	for s := range samples {
		counts["total"] += 1
		b, err := json.Marshal(s)
		if err != nil {
			counts["error"] += 1
			glog.Warningf("error marshalling sample: %s\n", err)
			continue
		}
		req := esapi.IndexRequest{
			Index:      esIndexName,
			DocumentID: getDocID(s),
			Body:       bytes.NewReader(b),
			Refresh:    "true",
		}
		res, err := req.Do(ctx, e.Client)
		if err != nil {
			counts["error"] += 1
			glog.Warningf("error exporting sample: %s\n", err)
			continue
		}
		res.Body.Close()

		counts["success"] += 1
		if counts["total"]%esSampleCountInfo == 0 {
			fmt.Printf("Sample export counts: %+v\n", counts)
		}
	}
	return nil
}
