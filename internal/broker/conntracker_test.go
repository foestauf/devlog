package broker

import (
	"net"
	"testing"
)

func TestConnTrackerRegisterAndDeregister(t *testing.T) {
	ct := newConnTracker()

	// Use pipe connections as stand-ins.
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ct.register(c1, 1)
	ct.register(c1, 2)
	ct.register(c1, 3)

	if ct.count() != 1 {
		t.Fatalf("expected 1 tracked connection, got %d", ct.count())
	}

	// Deregister returns all owned session IDs.
	ids := ct.deregister(c1)
	if len(ids) != 3 {
		t.Fatalf("expected 3 session IDs, got %d", len(ids))
	}
	if ct.count() != 0 {
		t.Fatalf("expected 0 tracked connections after deregister, got %d", ct.count())
	}
}

func TestConnTrackerUnregisterSession(t *testing.T) {
	ct := newConnTracker()

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ct.register(c1, 10)
	ct.register(c1, 20)
	ct.register(c1, 30)

	// Clean end of session 20.
	ct.unregisterSession(c1, 20)

	ids := ct.deregister(c1)
	if len(ids) != 2 {
		t.Fatalf("expected 2 remaining session IDs, got %d", len(ids))
	}
	for _, id := range ids {
		if id == 20 {
			t.Error("session 20 should have been unregistered")
		}
	}
}

func TestConnTrackerDeregisterUnknown(t *testing.T) {
	ct := newConnTracker()

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Deregistering an unknown connection returns nil.
	ids := ct.deregister(c1)
	if ids != nil {
		t.Fatalf("expected nil for unknown connection, got %v", ids)
	}
}
