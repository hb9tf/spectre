package rtlsdr

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/hb9tf/spectre/sdr"
)

const (
	SourceName = "rtlsdr"
	sweepAlias = "rtl_power"
)

type SDR struct {
	Identifier string
}

func (s SDR) Name() string {
	return SourceName
}

func (s *SDR) Sweep(opts *sdr.Options, samples chan<- sdr.Sample) error {
	args := []string{
		fmt.Sprintf("-f %d:%d:%d", opts.LowFreq, opts.HighFreq, opts.BinSize),
		fmt.Sprintf("-i %s", opts.IntegrationInterval),
		"-", // dumps samples to stdout
	}
	cmd := exec.Command(sweepAlias, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(out)
	// Start() executes command asynchronically.
	fmt.Printf("Running RTL SDR sweep: %q\n", cmd)
	if err := cmd.Start(); err != nil {
		glog.Exitf("unable to start sweep: %s\n", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			glog.Exitf("sweep command ended with error: %s\n", err)
		} else {
			glog.Exit("sweep command ended successfully")
		}
	}()

	// Start raw sample processing.
	for scanner.Scan() {
		if err := s.scanRow(scanner, samples); err != nil {
			glog.Warningf("error parsing line: %s\n", err)
			continue
		}
	}

	return nil
}

func parseUint(num string) (uint, error) {
	i, err := strconv.ParseUint(strings.Split(num, ".")[0], 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(i), nil
}

// calculateBinRange calculates the highest and lowest frequencies in a bin
func calculateBinRange(freqLow, freqHigh, binWidth, binNum uint) (uint, uint) {
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
		low, high := calculateBinRange(freqLow, freqHigh, binWidth, uint(i))
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
