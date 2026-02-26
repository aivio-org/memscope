package pipeline

import (
	"context"
	"time"

	"github.com/mbergo/memscope/internal/events"
)

// Pipeline wires normalizer + deduplicator + ring buffer together.
// It consumes a raw event channel (from the probe) and makes deduplicated,
// normalized events available via a ring buffer subscription.
type Pipeline struct {
	rb    *RingBuffer
	dedup *Deduplicator
}

// New creates a Pipeline backed by a ring buffer of the given capacity.
// Pass 0 to use the default capacity (65536).
func New(capacity int) *Pipeline {
	return &Pipeline{
		rb:    NewRingBuffer(capacity),
		dedup: NewDeduplicator(),
	}
}

// Run reads from src, normalizes, deduplicates, and pushes to the ring buffer
// until ctx is cancelled or src is closed.
func (p *Pipeline) Run(ctx context.Context, src <-chan events.MemEvent) {
	flushTicker := time.NewTicker(30 * time.Second)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-flushTicker.C:
			p.dedup.Flush(2 * time.Minute)
		case e, ok := <-src:
			if !ok {
				return
			}
			if out, keep := p.dedup.Process(e); keep {
				p.rb.Push(out)
			}
		}
	}
}

// RingBuffer returns the underlying ring buffer for subscriptions and draining.
func (p *Pipeline) RingBuffer() *RingBuffer { return p.rb }

// Subscribe is a convenience wrapper around RingBuffer.Subscribe.
func (p *Pipeline) Subscribe() <-chan events.MemEvent { return p.rb.Subscribe() }
