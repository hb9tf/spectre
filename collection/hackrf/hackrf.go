package hackrf

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/sdr"
)

const (
	SourceName = "hackrf"
	sweepAlias = "hackrf_sweep"
)

type SDR struct {
	Identifier string

	buckets   map[uint64]sdr.Sample
	bucketsMu *sync.Mutex
}

func (s SDR) Name() string {
	return SourceName
}

func (s *SDR) Sweep(opts *sdr.Options, samples chan<- sdr.Sample) error {
	s.buckets = map[uint64]sdr.Sample{}
	s.bucketsMu = &sync.Mutex{}

	args := []string{
		fmt.Sprintf("-f %d:%d", opts.LowFreq/1000000, opts.HighFreq/1000000),
		fmt.Sprintf("-w %d", opts.BinSize),
		"-a 1",  // RX RF amplifier 1=Enable, 0=Disable
		"-l 16", // RX LNA (IF) gain, 0-40dB, 8dB steps
		"-g 20", // RX VGA (baseband) gain, 0-62dB, 2dB steps
	}
	cmd := exec.Command(sweepAlias, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(out)
	// Start() executes command asynchronically.
	fmt.Printf("Running HackRF sweep: %q\n", cmd)
	if err := cmd.Start(); err != nil {
		glog.Fatalf("unable to start sweep: %s\n", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			glog.Exitf("sweep command ended with error: %s\n", err)
		} else {
			glog.Exit("sweep command ended successfully")
		}
	}()

	rawSamples := make(chan sdr.Sample)
	// Start raw sample processing.
	go func() {
		for scanner.Scan() {
			if err := s.scanRow(scanner, rawSamples); err != nil {
				glog.Warningf("error parsing line: %s\n", err)
				continue
			}
		}
	}()

	// Output aggregated samples in regular ticks.
	ticker := time.NewTicker(opts.IntegrationInterval)
	go func() {
		for range ticker.C {
			// This is not concurrency friendly... Buuut it's ok:
			// We're creating a new bucket to store new records in
			// and operate on the old one afterwards. Since we aggregate,
			// we won't miss much ¯\_(ツ)_/¯
			old := s.buckets
			s.bucketsMu.Lock()
			s.buckets = map[uint64]sdr.Sample{}
			s.bucketsMu.Unlock()

			for _, sample := range old {
				samples <- sample
			}
		}
	}()

	// Aggregate samples in frequency buckets.
	for sample := range rawSamples {
		stored, ok := s.buckets[sample.FreqCenter]
		if !ok {
			s.buckets[sample.FreqCenter] = sample
			continue
		}
		stored.End = sample.End
		stored.DBAvg = (stored.DBAvg*float64(stored.SampleCount) + sample.DBAvg*float64(sample.SampleCount)) / float64(stored.SampleCount+sample.SampleCount)
		if sample.DBLow < stored.DBLow {
			stored.DBLow = sample.DBLow
		}
		if sample.DBHigh > stored.DBHigh {
			stored.DBHigh = sample.DBHigh
		}
		stored.SampleCount += sample.SampleCount
		s.bucketsMu.Lock()
		s.buckets[sample.FreqCenter] = stored
		s.bucketsMu.Unlock()
	}

	return nil
}

func parseUint(num string) (uint64, error) {
	return strconv.ParseUint(strings.Split(num, ".")[0], 10, 64)
}

// calculateBinRange calculates the highest and lowest frequencies in a bin
func calculateBinRange(freqLow, freqHigh, binWidth, binNum uint64) (uint64, uint64) {
	low := freqLow + (binNum * binWidth)
	high := low + binWidth
	if high > freqHigh {
		high = freqHigh
	}
	return low, high
}
func (s *SDR) scanRow(scanner *bufio.Scanner, samples chan<- sdr.Sample) error {
	glog.V(3).Info(scanner.Text())
	row := strings.Split(scanner.Text(), ", ")
	numBins := len(row) - 6

	sampleCount, err := parseUint(row[5])
	if err != nil {
		return err
	}
	freqLow, err := parseUint(row[2])
	if err != nil {
		return err
	}
	freqHigh, err := parseUint(row[3])
	if err != nil {
		return err
	}
	binWidth, err := parseUint(row[4])
	if err != nil {
		return err
	}

	for i := 0; i < numBins; i++ {
		low, high := calculateBinRange(freqLow, freqHigh, binWidth, uint64(i))
		binRowIndex := i + 6
		parsedTime, err := time.Parse(time.RFC3339, row[0]+"T"+row[1]+"Z")
		if err != nil {
			return err
		}

		decibels, err := strconv.ParseFloat(row[binRowIndex], 64)
		if err != nil {
			return err
		}

		samples <- sdr.Sample{
			Identifier:  s.Identifier,
			Source:      s.Name(),
			FreqCenter:  (low + high) / 2,
			FreqLow:     low,
			FreqHigh:    high,
			DBLow:       decibels,
			DBHigh:      decibels,
			DBAvg:       decibels,
			SampleCount: sampleCount,
			Start:       parsedTime,
			End:         parsedTime,
		}
	}
	return nil
}
