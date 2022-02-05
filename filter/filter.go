package filter

import "github.com/hb9tf/spectre/sdr"

type Filterer interface {
	ShouldIgnore(*sdr.Sample) bool
}

func Filter(input <-chan sdr.Sample, output chan<- sdr.Sample, filters []Filterer) error {
	for s := range input {
		skip := false
		for _, f := range filters {
			skip = f.ShouldIgnore(&s)
		}
		if skip {
			continue
		}
		output <- s
	}
	return nil
}

type FilterFreq struct {
	FreqHigh int
	FreqLow  int
}

func (f *FilterFreq) ShouldIgnore(s *sdr.Sample) bool {
	// Check if low freq of sample is higher than what we want to include.
	if s.FreqLow > f.FreqHigh {
		return true
	}
	// Check if high freq of sample is lower than what we want to include.
	if s.FreqHigh < f.FreqLow {
		return true
	}
	return false
}
