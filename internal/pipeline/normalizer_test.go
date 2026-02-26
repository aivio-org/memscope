package pipeline_test

import (
	"sync"
	"testing"
	"time"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/pipeline"
)

func TestNormalizer_BasicConversion(t *testing.T) {
	n := pipeline.NewNormalizer()
	raw := pipeline.RawAllocEvent{
		Addr:        0xc000010000,
		Size:        256,
		GoroutineID: 7,
		TimestampNs: 0, // same as reference point
	}
	e := n.Normalize(raw, events.KindAlloc)

	if e.Kind != events.KindAlloc {
		t.Errorf("kind: want %v got %v", events.KindAlloc, e.Kind)
	}
	if e.Addr != raw.Addr {
		t.Errorf("addr: want %x got %x", raw.Addr, e.Addr)
	}
	if e.Size != raw.Size {
		t.Errorf("size: want %d got %d", raw.Size, e.Size)
	}
	if e.GoroutineID != raw.GoroutineID {
		t.Errorf("goroutine: want %d got %d", raw.GoroutineID, e.GoroutineID)
	}
}

func TestNormalizer_TimestampConversion(t *testing.T) {
	n := pipeline.NewNormalizer()

	// Set a reference: bpfNs=1000, wall=now
	refTime := time.Now()
	const refBPF = uint64(1_000_000_000) // 1 second in ns

	// Manually construct a SetBootReference call and check that an event
	// 500ms after the reference gets the right wall timestamp.
	n.SetBootReference(refBPF)

	raw := pipeline.RawAllocEvent{
		TimestampNs: refBPF + 500_000_000, // 500ms after reference
	}
	e := n.Normalize(raw, events.KindAlloc)

	// The wall timestamp should be within ~50ms of refTime + 500ms.
	want := refTime.Add(500 * time.Millisecond)
	diff := e.Timestamp.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > 50*time.Millisecond {
		t.Errorf("timestamp off by %v (want ~%v, got %v)", diff, want, e.Timestamp)
	}
}

func TestNormalizer_ConcurrentNoRace(t *testing.T) {
	t.Parallel()
	n := pipeline.NewNormalizer()

	var wg sync.WaitGroup
	const writers = 4
	const readers = 8

	// Concurrently call SetBootReference from multiple goroutines
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				n.SetBootReference(uint64(i*1000 + j))
			}
		}(i)
	}

	// Concurrently call Normalize from multiple goroutines
	var tsErrors int
	var mu sync.Mutex
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				raw := pipeline.RawAllocEvent{
					Addr:        uint64(j) * 0x1000,
					Size:        64,
					TimestampNs: uint64(j) * 1_000_000,
				}
				e := n.Normalize(raw, events.KindAlloc)
				// Timestamps must be in the past or very near future
				if e.Timestamp.After(time.Now().Add(time.Minute)) {
					mu.Lock()
					tsErrors++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	if tsErrors > 0 {
		t.Errorf("%d events had timestamps more than 1 minute in the future", tsErrors)
	}
}

func TestNormalizer_IndependentInstances(t *testing.T) {
	t.Parallel()
	// Two normalizers must not share state
	n1 := pipeline.NewNormalizer()
	n2 := pipeline.NewNormalizer()

	n1.SetBootReference(0)
	n2.SetBootReference(999_999_999_999) // far in the future

	raw := pipeline.RawAllocEvent{TimestampNs: 1_000_000_000}
	e1 := n1.Normalize(raw, events.KindAlloc)
	e2 := n2.Normalize(raw, events.KindAlloc)

	// e1 and e2 should have different timestamps since their references differ
	if e1.Timestamp.Equal(e2.Timestamp) {
		t.Error("independent normalizers produced identical timestamps")
	}
}
