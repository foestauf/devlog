package session

import (
	"testing"
)

func TestManagerRegisterAndGet(t *testing.T) {
	m := NewManager()
	s := &Session{ID: 1, Service: "api", PID: 1234, Command: "pnpm dev"}
	m.Register(s)

	got := m.Get(1)
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.Service != "api" {
		t.Errorf("expected service 'api', got %q", got.Service)
	}
}

func TestManagerGetMissing(t *testing.T) {
	m := NewManager()
	if got := m.Get(999); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestManagerDeregister(t *testing.T) {
	m := NewManager()
	m.Register(&Session{ID: 1, Service: "api"})
	m.Deregister(1)
	if got := m.Get(1); got != nil {
		t.Errorf("expected nil after deregister, got %v", got)
	}
	if m.Count() != 0 {
		t.Errorf("expected count 0, got %d", m.Count())
	}
}

func TestManagerActiveSessions(t *testing.T) {
	m := NewManager()
	m.Register(&Session{ID: 1, Service: "api"})
	m.Register(&Session{ID: 2, Service: "worker"})
	m.Register(&Session{ID: 3, Service: "api"})

	all := m.ActiveSessions()
	if len(all) != 3 {
		t.Errorf("expected 3 active sessions, got %d", len(all))
	}

	apiSessions := m.ActiveForService("api")
	if len(apiSessions) != 2 {
		t.Errorf("expected 2 api sessions, got %d", len(apiSessions))
	}

	workerSessions := m.ActiveForService("worker")
	if len(workerSessions) != 1 {
		t.Errorf("expected 1 worker session, got %d", len(workerSessions))
	}
}

func TestManagerCount(t *testing.T) {
	m := NewManager()
	if m.Count() != 0 {
		t.Errorf("expected 0, got %d", m.Count())
	}
	m.Register(&Session{ID: 1, Service: "api"})
	if m.Count() != 1 {
		t.Errorf("expected 1, got %d", m.Count())
	}
}
