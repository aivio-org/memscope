//go:build !linux

package agent

import "fmt"

// On non-Linux platforms eBPF is unavailable.
func newEBPFProbe(_ int) (Probe, error) {
	return nil, fmt.Errorf("eBPF probes are only supported on Linux (kernel ≥ 5.8)")
}
