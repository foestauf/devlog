package cache

// RingBuffer is a fixed-capacity circular buffer that overwrites the oldest
// items when full. It is not thread-safe on its own — callers are responsible
// for synchronisation.
type RingBuffer[T any] struct {
	buf   []T
	head  int // next write position
	count int
	cap   int
}

// NewRingBuffer creates a ring buffer with the given capacity.
// Capacity must be at least 1; if zero or negative it is clamped to 1.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer[T]{
		buf: make([]T, capacity),
		cap: capacity,
	}
}

// Push adds an item to the buffer, overwriting the oldest entry when full.
func (r *RingBuffer[T]) Push(item T) {
	r.buf[r.head] = item
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
}

// Recent returns up to n most recent items in chronological (oldest-first) order.
// If n exceeds the number of stored items, all stored items are returned.
func (r *RingBuffer[T]) Recent(n int) []T {
	if n <= 0 {
		return nil
	}
	if n > r.count {
		n = r.count
	}

	result := make([]T, n)
	// Start index: the oldest of the n items we want.
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		result[i] = r.buf[(start+i)%r.cap]
	}
	return result
}

// Len returns the number of items currently in the buffer.
func (r *RingBuffer[T]) Len() int {
	return r.count
}

// Clear removes all items from the buffer.
func (r *RingBuffer[T]) Clear() {
	var zero T
	for i := range r.buf {
		r.buf[i] = zero
	}
	r.head = 0
	r.count = 0
}
