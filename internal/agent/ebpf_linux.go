//go:build linux

package agent

import (
	"github.com/mbergo/memscope/internal/agent/gobpf"
)

func newEBPFProbe(_ int) (Probe, error) {
	return &gobpf.Probe{}, nil
}
