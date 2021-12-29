package export

import (
	"context"

	"github.com/hb9tf/spectre/sdr"
)

type Exporter interface {
	Write(context.Context, <-chan sdr.Sample) error
}
