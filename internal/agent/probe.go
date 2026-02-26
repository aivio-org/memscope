package agent

import (
	"github.com/mbergo/memscope/internal/events"
)

// Probe is the interface implemented by the real eBPF probe.
type Probe interface {
	// Start attaches to the target PID and returns two channels:
	//   - memCh: normalized MemEvents (alloc, dealloc, GC, stack-grow)
	//   - sysCh: SyscallEvents captured via raw_tracepoint
	// Both channels are closed when Stop() is called or the target exits.
	Start(pid int) (memCh <-chan events.MemEvent, sysCh <-chan events.SyscallEvent, err error)

	// Stop detaches all probes and releases kernel resources.
	Stop() error

	// Lang reports the detected target language ("go", "rust", "unknown").
	Lang() string
}

// New returns a real eBPF Probe for the given PID.
// Requires Linux with CAP_BPF, CAP_PERFMON, and CAP_SYS_PTRACE.
func New(pid int) (Probe, error) {
	return newEBPFProbe(pid)
}

// newEBPFProbe is defined in ebpf_linux.go (linux) or ebpf_stub.go (other).
