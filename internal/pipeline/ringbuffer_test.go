package pipeline_test

import (
	"testing"
	"time"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/pipeline"
)

func makeEvent(addr uint64, size uint64, kind events.EventKind) events.MemEvent {
	return events.MemEvent{
		Kind:      kind,
		Addr:      addr,
		Size:      size,
		Timestamp: time.Now(),
	}
}

func TestRingBuffer_PushDrain(t *testing.T) {
	rb := pipeline.NewRingBuffer(8)

	for i := 0; i < 5; i++ {
		rb.Push(makeEvent(uint64(i), 100, events.KindAlloc))
	}

	if rb.Len() != 5 {
		t.Fatalf("expected 5 events, got %d", rb.Len())
	}

	evts := rb.Drain(3)
	if len(evts) != 3 {
		t.Fatalf("expected 3 drained events, got %d", len(evts))
	}
	if rb.Len() != 2 {
		t.Fatalf("expected 2 remaining, got %d", rb.Len())
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := pipeline.NewRingBuffer(4)

	// Push more than capacity
	for i := 0; i < 6; i++ {
		rb.Push(makeEvent(uint64(i), uint64(i*10), events.KindAlloc))
	}

	// Buffer should be capped at 4
	if rb.Len() != 4 {
		t.Fatalf("expected 4 events after overflow, got %d", rb.Len())
	}

	// FIFO: oldest dropped, so addresses should be 2,3,4,5
	evts := rb.Drain(0)
	if len(evts) != 4 {
		t.Fatalf("expected 4 events from full drain, got %d", len(evts))
	}
	if evts[0].Addr != 2 {
		t.Errorf("expected addr=2 (oldest surviving), got %d", evts[0].Addr)
	}
	if evts[3].Addr != 5 {
		t.Errorf("expected addr=5 (newest), got %d", evts[3].Addr)
	}
}

func TestRingBuffer_DrainEmpty(t *testing.T) {
	rb := pipeline.NewRingBuffer(16)
	evts := rb.Drain(10)
	if evts != nil {
		t.Errorf("expected nil from empty drain, got %v", evts)
	}
}

func TestRingBuffer_Subscribe(t *testing.T) {
	rb := pipeline.NewRingBuffer(16)
	sub := rb.Subscribe()

	e := makeEvent(0xdeadbeef, 512, events.KindAlloc)
	rb.Push(e)

	select {
	case received := <-sub:
		if received.Addr != e.Addr {
			t.Errorf("subscriber got wrong addr: %x", received.Addr)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber did not receive event")
	}
}
