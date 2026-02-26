package pipeline

import (
	"sync/atomic"
	"time"

	"github.com/mbergo/memscope/internal/events"
)

// RawAllocEvent mirrors the C struct pushed by the eBPF program.
// Fields are little-endian uint64 matching the BPF map layout.
type RawAllocEvent struct {
	Addr        uint64
	Size        uint64
	GoroutineID uint64
	TimestampNs uint64 // nanoseconds since system boot (bpf_ktime_get_ns)
}

// bootRef holds both fields of the time-conversion reference as a single
// immutable value so they can be swapped atomically with no torn-update window.
type bootRef struct {
	wall  time.Time
	bpfNs uint64
}

// Normalizer converts raw BPF events to wall-clock MemEvents.
// Use NewNormalizer; the zero value uses time.Now() as the boot reference.
type Normalizer struct {
	ref atomic.Pointer[bootRef]
}

// NewNormalizer returns a Normalizer anchored to the current wall time.
func NewNormalizer() *Normalizer {
	n := &Normalizer{}
	n.ref.Store(&bootRef{wall: time.Now(), bpfNs: 0})
	return n
}

// SetBootReference records the BPF timestamp observed at attach time and the
// corresponding wall-clock time as an atomic unit. Safe to call concurrently
// with Normalize.
func (n *Normalizer) SetBootReference(bpfNs uint64) {
	n.ref.Store(&bootRef{wall: time.Now(), bpfNs: bpfNs})
}

// Normalize converts a RawAllocEvent into a MemEvent.
// TypeName and file/line resolution are deferred to Phase 3 (DWARF).
func (n *Normalizer) Normalize(raw RawAllocEvent, kind events.EventKind) events.MemEvent {
	ref := n.ref.Load()

	// wallTime = ref.wall + (raw.TimestampNs - ref.bpfNs)
	delta := time.Duration(raw.TimestampNs-ref.bpfNs) * time.Nanosecond
	ts := ref.wall.Add(delta)

	// Sanity clamp: reject timestamps more than 1 minute in the future.
	if now := time.Now(); ts.After(now.Add(time.Minute)) {
		ts = now
	}

	return events.MemEvent{
		Kind:        kind,
		Addr:        raw.Addr,
		Size:        raw.Size,
		GoroutineID: raw.GoroutineID,
		Timestamp:   ts,
		// TypeName, SourceFile, SourceLine — filled by DWARF resolver (Phase 3)
	}
}

// Package-level shim so gobpf/ebpf.go (which calls SetBootReference as a free
// function) continues to compile without changes until Phase 2 wires the
// Normalizer struct through the probe.
var defaultNormalizer = NewNormalizer()

// SetBootReference is a package-level shim that delegates to the default
// Normalizer. Deprecated: inject *Normalizer via Pipeline.New in Phase 2.
func SetBootReference(bpfNs uint64) { defaultNormalizer.SetBootReference(bpfNs) }

// Normalize is a package-level shim for gobpf/ebpf.go.
// Deprecated: use (*Normalizer).Normalize in Phase 2.
func Normalize(raw RawAllocEvent, kind events.EventKind) events.MemEvent {
	return defaultNormalizer.Normalize(raw, kind)
}

// BootReference is the exported view of the current time-conversion reference.
type BootReference struct {
	Wall  time.Time
	BpfNs uint64
}

// BootRef returns the current boot reference snapshot so callers outside the
// pipeline can convert BPF timestamps to wall time without calling Normalize.
func BootRef() BootReference {
	ref := defaultNormalizer.ref.Load()
	return BootReference{Wall: ref.wall, BpfNs: ref.bpfNs}
}

// BpfNsToWall converts a BPF ktime timestamp to a wall-clock time using the
// given reference. Clamps timestamps more than 1 minute in the future.
func BpfNsToWall(bpfNs uint64, ref BootReference) time.Time {
	delta := time.Duration(int64(bpfNs-ref.BpfNs)) * time.Nanosecond
	ts := ref.Wall.Add(delta)
	if now := time.Now(); ts.After(now.Add(time.Minute)) {
		ts = now
	}
	return ts
}
