package broker

import (
	"net"
	"sync"
)

// connTracker maps active connections to the session IDs they own.
// When a connection drops unexpectedly, the broker uses this to clean up
// orphaned sessions.
type connTracker struct {
	mu    sync.Mutex
	conns map[net.Conn][]int64
}

func newConnTracker() *connTracker {
	return &connTracker{
		conns: make(map[net.Conn][]int64),
	}
}

// register associates a session ID with a connection.
func (ct *connTracker) register(conn net.Conn, sessionID int64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.conns[conn] = append(ct.conns[conn], sessionID)
}

// unregisterSession removes a specific session ID from a connection's list.
// Called when a client sends a clean session_end.
func (ct *connTracker) unregisterSession(conn net.Conn, sessionID int64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ids := ct.conns[conn]
	for i, id := range ids {
		if id == sessionID {
			ct.conns[conn] = append(ids[:i], ids[i+1:]...)
			return
		}
	}
}

// deregister removes a connection entirely and returns all session IDs
// that were still associated with it (i.e. not cleanly ended).
func (ct *connTracker) deregister(conn net.Conn) []int64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ids := ct.conns[conn]
	delete(ct.conns, conn)
	return ids
}

// count returns the number of tracked connections.
func (ct *connTracker) count() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return len(ct.conns)
}
