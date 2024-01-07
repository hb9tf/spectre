package sdr

import (
	"time"
)

type Sample struct {
	// Metadata
	Identifier string
	Source     string

	// Radio Data
	FreqCenter  int64
	FreqLow     int64
	FreqHigh    int64
	DBHigh      float64
	DBLow       float64
	DBAvg       float64
	SampleCount int64
	Start       time.Time
	End         time.Time
}

type SDR interface {
	Name() string
	Sweep(opts *Options, samples chan<- Sample) error
}

type Options struct {
	// LowFreq is the lower frequency to start the sweeps with in Hz.
	LowFreq int64
	// LowFreq is the upper frequency to end the sweeps with in Hz.
	HighFreq int64

	// BinSize is the FFT bin width (frequency resolution) in Hz.
	// BinSize is a maximum, smaller more convenient bins will be used.
	BinSize int64

	// IntegrationInterval is the duration during which to collect information per frequency.
	IntegrationInterval time.Duration
}
