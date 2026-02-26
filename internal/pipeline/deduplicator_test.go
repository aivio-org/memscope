package pipeline_test

import (
	"testing"
	"time"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/pipeline"
)

func TestDeduplicator_AllocPassthrough(t *testing.T) {
	d := pipeline.NewDeduplicator()

	alloc := events.MemEvent{
		Kind:      events.KindAlloc,
		Addr:      0x1000,
		Size:      256,
		Timestamp: time.Now(),
	}
	out, keep := d.Process(alloc)
	if !keep {
		t.Fatal("alloc event should be kept")
	}
	if out.Addr != alloc.Addr {
		t.Errorf("addr mismatch")
	}
}

func TestDeduplicator_ShortLivedDropped(t *testing.T) {
	d := pipeline.NewDeduplicator()

	ts := time.Now()
	alloc := events.MemEvent{
		Kind:      events.KindAlloc,
		Addr:      0x2000,
		Size:      64,
		Timestamp: ts,
	}
	d.Process(alloc)

	// Dealloc immediately (< 1ms)
	dealloc := events.MemEvent{
		Kind:      events.KindDealloc,
		Addr:      0x2000,
		Size:      64,
		Timestamp: ts.Add(100 * time.Microsecond),
	}
	_, keep := d.Process(dealloc)
	if keep {
		t.Fatal("short-lived alloc+free pair should suppress the dealloc")
	}
}

func TestDeduplicator_LongLivedKept(t *testing.T) {
	d := pipeline.NewDeduplicator()

	ts := time.Now()
	alloc := events.MemEvent{
		Kind:      events.KindAlloc,
		Addr:      0x3000,
		Size:      1024,
		Timestamp: ts,
	}
	d.Process(alloc)

	// Dealloc well after 1ms
	dealloc := events.MemEvent{
		Kind:      events.KindDealloc,
		Addr:      0x3000,
		Size:      1024,
		Timestamp: ts.Add(5 * time.Millisecond),
	}
	_, keep := d.Process(dealloc)
	if !keep {
		t.Fatal("long-lived alloc should not be suppressed on free")
	}
}

func TestDeduplicator_GCAlwaysKept(t *testing.T) {
	d := pipeline.NewDeduplicator()

	pause := events.MemEvent{
		Kind:      events.KindGCPause,
		Timestamp: time.Now(),
	}
	_, keep := d.Process(pause)
	if !keep {
		t.Fatal("GC events should always pass through")
	}
}

func TestDeduplicator_StackGrowNotTracked(t *testing.T) {
	d := pipeline.NewDeduplicator()

	// Emit 100 stack-grow events — inflight map must not grow
	for i := 0; i < 100; i++ {
		e := events.MemEvent{
			Kind:      events.KindStackGrow,
			Addr:      uint64(i) * 0x1000,
			Size:      4096,
			Timestamp: time.Now(),
		}
		_, keep := d.Process(e)
		if !keep {
			t.Fatalf("stack-grow event %d should pass through", i)
		}
	}

	if d.InFlight() != 0 {
		t.Errorf("KindStackGrow events must not be tracked in inflight; got %d", d.InFlight())
	}
}

func TestDeduplicator_Flush(t *testing.T) {
	d := pipeline.NewDeduplicator()

	// Insert stale alloc
	alloc := events.MemEvent{
		Kind:      events.KindAlloc,
		Addr:      0x4000,
		Size:      32,
		Timestamp: time.Now().Add(-3 * time.Minute),
	}
	d.Process(alloc)

	if d.InFlight() != 1 {
		t.Fatal("expected 1 in-flight alloc before flush")
	}

	dropped := d.Flush(2 * time.Minute)
	if dropped != 1 {
		t.Errorf("expected 1 flushed, got %d", dropped)
	}
	if d.InFlight() != 0 {
		t.Fatal("expected 0 in-flight after flush")
	}
}
