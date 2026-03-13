package cache

import (
	"sort"
	"sync"
)

// LogEntry represents a single captured log line.
type LogEntry struct {
	ID        int64
	SessionID int64
	Timestamp int64
	Stream    string
	Level     string
	Message   string
	Service   string
}

// serviceBuffer wraps a ring buffer with its own mutex so we can lock
// per-service rather than holding the top-level lock during writes.
type serviceBuffer struct {
	mu  sync.RWMutex
	buf *RingBuffer[LogEntry]
}

// LogCache manages per-service ring buffers for fast recent-log access.
// It is safe for concurrent use.
type LogCache struct {
	mu         sync.RWMutex
	buffers    map[string]*serviceBuffer
	bufferSize int
}

// NewLogCache creates a LogCache where each service gets a ring buffer of
// the given size.
func NewLogCache(bufferSize int) *LogCache {
	return &LogCache{
		buffers:    make(map[string]*serviceBuffer),
		bufferSize: bufferSize,
	}
}

// getOrCreate returns the buffer for a service, creating one if it doesn't
// exist yet. Caller must NOT hold lc.mu.
func (lc *LogCache) getOrCreate(service string) *serviceBuffer {
	// Fast path: read lock.
	lc.mu.RLock()
	sb, ok := lc.buffers[service]
	lc.mu.RUnlock()
	if ok {
		return sb
	}

	// Slow path: write lock to insert.
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Double-check after acquiring write lock.
	if sb, ok = lc.buffers[service]; ok {
		return sb
	}

	sb = &serviceBuffer{
		buf: NewRingBuffer[LogEntry](lc.bufferSize),
	}
	lc.buffers[service] = sb
	return sb
}

// Add inserts a log entry into the appropriate service buffer.
func (lc *LogCache) Add(entry LogEntry) {
	sb := lc.getOrCreate(entry.Service)
	sb.mu.Lock()
	sb.buf.Push(entry)
	sb.mu.Unlock()
}

// Recent returns up to limit recent entries for the named service in
// chronological order. Returns nil if the service is unknown.
func (lc *LogCache) Recent(service string, limit int) []LogEntry {
	lc.mu.RLock()
	sb, ok := lc.buffers[service]
	lc.mu.RUnlock()
	if !ok {
		return nil
	}

	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.Recent(limit)
}

// Services returns a sorted list of known service names.
func (lc *LogCache) Services() []string {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	names := make([]string, 0, len(lc.buffers))
	for name := range lc.buffers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// EntryCount returns the number of entries in a service's ring buffer,
// or 0 if the service is unknown.
func (lc *LogCache) EntryCount(service string) int {
	lc.mu.RLock()
	sb, ok := lc.buffers[service]
	lc.mu.RUnlock()
	if !ok {
		return 0
	}

	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.Len()
}

// Clear removes all entries for a specific service.
func (lc *LogCache) Clear(service string) {
	lc.mu.RLock()
	sb, ok := lc.buffers[service]
	lc.mu.RUnlock()
	if !ok {
		return
	}

	sb.mu.Lock()
	sb.buf.Clear()
	sb.mu.Unlock()
}

// ClearAll removes every service buffer.
func (lc *LogCache) ClearAll() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.buffers = make(map[string]*serviceBuffer)
}
