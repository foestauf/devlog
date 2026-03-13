package client

import "sync"

const lineBufferCapacity = 1000

// bufferedLine holds a log line that couldn't be sent to the broker.
type bufferedLine struct {
	Stream    string
	Message   string
	Timestamp int64
}

// lineBuffer is a fixed-capacity ring buffer of log lines used for
// client-side buffering during broker downtime. Thread-safe.
type lineBuffer struct {
	mu    sync.Mutex
	buf   []bufferedLine
	head  int
	count int
}

func newLineBuffer() *lineBuffer {
	return &lineBuffer{
		buf: make([]bufferedLine, lineBufferCapacity),
	}
}

// push adds a line to the buffer, overwriting the oldest if full.
func (lb *lineBuffer) push(line bufferedLine) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.buf[lb.head] = line
	lb.head = (lb.head + 1) % lineBufferCapacity
	if lb.count < lineBufferCapacity {
		lb.count++
	}
}

// drain returns all buffered lines in chronological order and clears the
// buffer. Returns nil if empty.
func (lb *lineBuffer) drain() []bufferedLine {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.count == 0 {
		return nil
	}

	result := make([]bufferedLine, lb.count)
	start := (lb.head - lb.count + lineBufferCapacity) % lineBufferCapacity
	for i := 0; i < lb.count; i++ {
		result[i] = lb.buf[(start+i)%lineBufferCapacity]
	}

	lb.head = 0
	lb.count = 0
	return result
}

// len returns the number of buffered lines.
func (lb *lineBuffer) len() int {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.count
}
