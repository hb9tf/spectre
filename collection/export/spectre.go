package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang/glog"
	"github.com/hb9tf/spectre/collection/sdr"
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
		resp.Body.Close()
	}

	return nil
}
