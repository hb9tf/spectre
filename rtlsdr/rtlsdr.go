package rtlsdr

import (
	"bufio"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hb9tf/spectre/sdr"
)

const (
	sourceName = "rtl_sdr"
	sweepAlias = "rtl_power"
)

type SDR struct {
	Identifier string
}

func (s SDR) Name() string {
	return sourceName
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
		log.Panicln(err)
	}

	// Start raw sample processing.
	for scanner.Scan() {
		if err := s.scanRow(scanner, samples); err != nil {
			log.Println(err)
			continue
		}
	}

	return nil
}

func parseInt(num string) (int, error) {
	return strconv.Atoi(strings.Split(num, ".")[0])
}

// calculateBinRange calculates the highest and lowest frequencies in a bin
func calculateBinRange(freqLow, freqHigh, binWidth, binNum int) (int, int) {
	low := freqLow + (binNum * binWidth)
	high := low + binWidth
	if high > freqHigh {
		high = freqHigh
	}
	return low, high
}
func (s *SDR) scanRow(scanner *bufio.Scanner, samples chan<- sdr.Sample) error {
	row := strings.Split(scanner.Text(), ", ")
	numBins := len(row) - 6

	sampleCount, err := parseInt(row[5])
	if err != nil {
		return err
	}
	freqLow, err := parseInt(row[2])
	if err != nil {
		return err
	}
	freqHigh, err := parseInt(row[3])
	if err != nil {
		return err
	}
	binWidth, err := parseInt(row[4])
	if err != nil {
		return err
	}

	for i := 0; i < numBins; i++ {
		low, high := calculateBinRange(freqLow, freqHigh, binWidth, i)
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
