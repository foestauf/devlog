package session

import (
	"sync"
	"time"
)

// Session represents an active client session with the broker.
type Session struct {
	ID        int64
	Service   string
	PID       int
	Command   string
	StartedAt int64
	Hostname  string
}

// Manager tracks active sessions in memory.
type Manager struct {
	mu       sync.RWMutex
	sessions map[int64]*Session
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[int64]*Session),
	}
}

// Register adds an active session.
func (m *Manager) Register(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
}

// Deregister removes a session by ID.
func (m *Manager) Deregister(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// Get returns a session by ID, or nil if not found.
func (m *Manager) Get(id int64) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// ActiveSessions returns all currently active sessions.
func (m *Manager) ActiveSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// ActiveForService returns active sessions for a specific service.
func (m *Manager) ActiveForService(service string) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Session
	for _, s := range m.sessions {
		if s.Service == service {
			result = append(result, s)
		}
	}
	return result
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Now returns the current unix timestamp in milliseconds.
func Now() int64 {
	return time.Now().UnixMilli()
}
