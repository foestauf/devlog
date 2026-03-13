package client

import "testing"

func TestLineBufferPushAndDrain(t *testing.T) {
	lb := newLineBuffer()

	lb.push(bufferedLine{Stream: "stdout", Message: "line 1", Timestamp: 1})
	lb.push(bufferedLine{Stream: "stderr", Message: "line 2", Timestamp: 2})
	lb.push(bufferedLine{Stream: "stdout", Message: "line 3", Timestamp: 3})

	if lb.len() != 3 {
		t.Fatalf("expected len 3, got %d", lb.len())
	}

	lines := lb.drain()
	if len(lines) != 3 {
		t.Fatalf("expected 3 drained lines, got %d", len(lines))
	}
	if lines[0].Message != "line 1" {
		t.Errorf("expected first line 'line 1', got %q", lines[0].Message)
	}
	if lines[2].Message != "line 3" {
		t.Errorf("expected third line 'line 3', got %q", lines[2].Message)
	}

	// Buffer should be empty after drain.
	if lb.len() != 0 {
		t.Fatalf("expected len 0 after drain, got %d", lb.len())
	}
	if lines := lb.drain(); lines != nil {
		t.Fatalf("expected nil drain on empty buffer, got %d lines", len(lines))
	}
}

func TestLineBufferOverflow(t *testing.T) {
	lb := newLineBuffer()

	// Push more than capacity — oldest should be dropped.
	for i := 0; i < lineBufferCapacity+100; i++ {
		lb.push(bufferedLine{Stream: "stdout", Message: "x", Timestamp: int64(i)})
	}

	if lb.len() != lineBufferCapacity {
		t.Fatalf("expected len %d at capacity, got %d", lineBufferCapacity, lb.len())
	}

	lines := lb.drain()
	if len(lines) != lineBufferCapacity {
		t.Fatalf("expected %d drained lines, got %d", lineBufferCapacity, len(lines))
	}

	// First line should be the 101st pushed (index 100), not the first.
	if lines[0].Timestamp != 100 {
		t.Errorf("expected first timestamp 100 (oldest surviving), got %d", lines[0].Timestamp)
	}
	// Last should be the newest.
	if lines[len(lines)-1].Timestamp != int64(lineBufferCapacity+99) {
		t.Errorf("expected last timestamp %d, got %d", lineBufferCapacity+99, lines[len(lines)-1].Timestamp)
	}
}
