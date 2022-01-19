package sdr

import (
	"time"
)

type Sample struct {
	// Metadata
	Identifier string
	Source     string

	// Radio Data
	FreqCenter  int
	FreqLow     int
	FreqHigh    int
	DBHigh      float64
	DBLow       float64
	DBAvg       float64
	SampleCount int
	Start       time.Time
	End         time.Time
}

type SDR interface {
	Name() string
	Sweep(opts *Options, samples chan<- Sample) error
}

type Options struct {
	// LowFreq is the lower frequency to start the sweeps with in Hz.
	LowFreq int
	// LowFreq is the upper frequency to end the sweeps with in Hz.
	HighFreq int

	// BinSize is the FFT bin width (frequency resolution) in Hz.
	// BinSize is a maximum, smaller more convenient bins will be used.
	BinSize int

	// IntegrationInterval is the duration during which to collect information per frequency.
	IntegrationInterval time.Duration
}
