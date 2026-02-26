package pipeline

import (
	"sync"
	"time"

	"github.com/mbergo/memscope/internal/events"
)

const dedupeWindow = time.Millisecond

// Deduplicator drops alloc+free pairs that occur within dedupeWindow of each
// other. This suppresses noise from very short-lived temporary allocations.
type Deduplicator struct {
	mu      sync.Mutex
	inflight map[uint64]events.MemEvent // keyed by Addr
}

// NewDeduplicator creates a ready-to-use Deduplicator.
func NewDeduplicator() *Deduplicator {
	return &Deduplicator{
		inflight: make(map[uint64]events.MemEvent),
	}
}

// Process filters the event stream. Returns (event, true) when the event
// should be forwarded, or (zero, false) when both the alloc and dealloc are
// suppressed.
func (d *Deduplicator) Process(e events.MemEvent) (events.MemEvent, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch e.Kind {
	case events.KindStackGrow:
		// Stack growth events have no paired KindDealloc (stacks are reclaimed
		// by GC, not by an explicit free uprobe). Pass through without tracking
		// to prevent unbounded inflight map growth.
		return e, true

	case events.KindAlloc:
		// Record in-flight allocation so a matching dealloc can be deduplicated.
		d.inflight[e.Addr] = e
		return e, true

	case events.KindDealloc:
		alloc, ok := d.inflight[e.Addr]
		if ok {
			delete(d.inflight, e.Addr)
			age := e.Timestamp.Sub(alloc.Timestamp)
			if age < dedupeWindow {
				// Suppress the dealloc for very short-lived allocations.
				// The alloc was already forwarded; callers must handle the
				// case where a dealloc never arrives (e.g. RemoveAlloc is
				// idempotent and no-ops on unknown addresses).
				return events.MemEvent{}, false
			}
		}
		return e, true

	default:
		// GC events and others always pass through
		return e, true
	}
}

// Flush removes stale in-flight entries older than maxAge to prevent unbounded
// growth. Call periodically (e.g., every 30s).
func (d *Deduplicator) Flush(maxAge time.Duration) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	dropped := 0
	for addr, e := range d.inflight {
		if now.Sub(e.Timestamp) > maxAge {
			delete(d.inflight, addr)
			dropped++
		}
	}
	return dropped
}

// InFlight returns the number of pending (unfreed) allocations.
func (d *Deduplicator) InFlight() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.inflight)
}
