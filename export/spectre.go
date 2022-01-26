package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"
	"github.com/hb9tf/spectre/sdr"
)

const (
	contentType             = "application/json"
	spectreEndpoint         = "spectre/v1/collect"
	defaultSendSampleAmount = 100
)

type SpectreServer struct {
	Server            string
	SendSamplesAmount int
}

func (s *SpectreServer) Write(ctx context.Context, samples <-chan sdr.Sample) error {

	type collectResponse struct {
		Status      string `json:"status"`
		SampleCount int    `json:"sampleCount"`
	}

	sendSamplesAmount := defaultSendSampleAmount
	if s.SendSamplesAmount > 0 {
		sendSamplesAmount = s.SendSamplesAmount
	}

	var samplesToSend []sdr.Sample
	for sample := range samples {
		samplesToSend = append(samplesToSend, sample)
		if len(samplesToSend) < sendSamplesAmount {
			continue // we haven't collected enough samples to send yet
		}

		body, err := json.Marshal(samplesToSend)
		if err != nil {
			glog.Warningf("error marshalling sample to JSON: %s\n", err)
			continue
		}

		resp, err := http.Post(fmt.Sprintf("%s/%s", strings.TrimRight(s.Server, "/"), spectreEndpoint), contentType, bytes.NewBuffer(body))
		if err != nil {
			glog.Warningf("error POSTing sample: %s\n", err)
			continue
		}
		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			glog.Warningf("error reading POST body: %s\n", err)
		}

		collectResponseBody := collectResponse{}
		json.Unmarshal(respBody, &collectResponseBody)
		glog.Infof("submitted %v samples to server %s", collectResponseBody.SampleCount, s.Server)

		resp.Body.Close()

		samplesToSend = nil
	}

	return nil
}
