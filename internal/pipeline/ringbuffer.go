package pipeline

import (
	"sync"

	"github.com/mbergo/memscope/internal/events"
)

const defaultCapacity = 65536

// RingBuffer is a thread-safe fixed-capacity circular buffer for MemEvents.
// When full, the oldest event is overwritten.
type RingBuffer struct {
	mu       sync.Mutex
	buf      []events.MemEvent
	head     int // next write position
	tail     int // next read position
	size     int // current number of elements
	cap      int
	subs     []chan events.MemEvent
}

// NewRingBuffer creates a RingBuffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &RingBuffer{
		buf: make([]events.MemEvent, capacity),
		cap: capacity,
	}
}

// Push adds an event to the ring buffer. If the buffer is full, the oldest
// event is silently dropped to make room.
func (rb *RingBuffer) Push(e events.MemEvent) {
	rb.mu.Lock()
	rb.buf[rb.head] = e
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	} else {
		// Overwrite oldest: advance tail
		rb.tail = (rb.tail + 1) % rb.cap
	}
	subs := rb.subs
	rb.mu.Unlock()

	// Fan-out to subscribers (non-blocking)
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Drain reads up to max events from the buffer without blocking.
// Returns events in FIFO order (oldest first).
func (rb *RingBuffer) Drain(max int) []events.MemEvent {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := rb.size
	if max > 0 && n > max {
		n = max
	}
	if n == 0 {
		return nil
	}

	out := make([]events.MemEvent, n)
	for i := range out {
		out[i] = rb.buf[rb.tail]
		rb.tail = (rb.tail + 1) % rb.cap
		rb.size--
	}
	return out
}

// Len returns the current number of events in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size
}

// Subscribe returns a buffered channel that receives a copy of every pushed event.
// The channel has a buffer of 4096; slow consumers will drop events.
func (rb *RingBuffer) Subscribe() <-chan events.MemEvent {
	ch := make(chan events.MemEvent, 4096)
	rb.mu.Lock()
	rb.subs = append(rb.subs, ch)
	rb.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscription channel.
func (rb *RingBuffer) Unsubscribe(sub <-chan events.MemEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for i, ch := range rb.subs {
		if ch == sub {
			rb.subs = append(rb.subs[:i], rb.subs[i+1:]...)
			close(ch)
			return
		}
	}
}
