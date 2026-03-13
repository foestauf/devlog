package cache

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// RingBuffer tests
// ---------------------------------------------------------------------------

func TestRingBuffer_PushAndRecent(t *testing.T) {
	rb := NewRingBuffer[int](5)

	rb.Push(1)
	rb.Push(2)
	rb.Push(3)

	got := rb.Recent(3)
	want := []int{1, 2, 3}
	assertInts(t, got, want)
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer[int](3)

	for i := 1; i <= 5; i++ {
		rb.Push(i)
	}

	if rb.Len() != 3 {
		t.Fatalf("expected Len()=3 after overflow, got %d", rb.Len())
	}

	got := rb.Recent(3)
	want := []int{3, 4, 5}
	assertInts(t, got, want)
}

func TestRingBuffer_RecentMoreThanExist(t *testing.T) {
	rb := NewRingBuffer[int](10)
	rb.Push(42)

	got := rb.Recent(100)
	want := []int{42}
	assertInts(t, got, want)
}

func TestRingBuffer_EmptyBuffer(t *testing.T) {
	rb := NewRingBuffer[int](5)

	if rb.Len() != 0 {
		t.Fatalf("expected empty buffer, got Len()=%d", rb.Len())
	}

	got := rb.Recent(5)
	if len(got) != 0 {
		t.Fatalf("expected empty slice from empty buffer, got %v", got)
	}
}

func TestRingBuffer_RecentZeroOrNegative(t *testing.T) {
	rb := NewRingBuffer[int](5)
	rb.Push(1)

	if got := rb.Recent(0); got != nil {
		t.Fatalf("Recent(0) should return nil, got %v", got)
	}
	if got := rb.Recent(-1); got != nil {
		t.Fatalf("Recent(-1) should return nil, got %v", got)
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer[int](5)
	rb.Push(1)
	rb.Push(2)
	rb.Clear()

	if rb.Len() != 0 {
		t.Fatalf("expected Len()=0 after Clear, got %d", rb.Len())
	}

	got := rb.Recent(5)
	if len(got) != 0 {
		t.Fatalf("expected no items after Clear, got %v", got)
	}

	// Ensure we can still push after clearing.
	rb.Push(10)
	assertInts(t, rb.Recent(1), []int{10})
}

func TestRingBuffer_ExactCapacity(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Push(1)
	rb.Push(2)
	rb.Push(3)

	if rb.Len() != 3 {
		t.Fatalf("expected Len()=3, got %d", rb.Len())
	}
	assertInts(t, rb.Recent(3), []int{1, 2, 3})
}

func TestRingBuffer_CapacityClamp(t *testing.T) {
	rb := NewRingBuffer[int](0)
	rb.Push(1)
	rb.Push(2)

	// Should behave as capacity 1.
	if rb.Len() != 1 {
		t.Fatalf("expected Len()=1 for clamped capacity, got %d", rb.Len())
	}
	assertInts(t, rb.Recent(1), []int{2})
}

// ---------------------------------------------------------------------------
// LogCache tests
// ---------------------------------------------------------------------------

func TestLogCache_PerServiceIsolation(t *testing.T) {
	lc := NewLogCache(100)

	lc.Add(LogEntry{ID: 1, Service: "api", Message: "hello from api"})
	lc.Add(LogEntry{ID: 2, Service: "worker", Message: "hello from worker"})

	apiEntries := lc.Recent("api", 10)
	if len(apiEntries) != 1 || apiEntries[0].Message != "hello from api" {
		t.Fatalf("unexpected api entries: %+v", apiEntries)
	}

	workerEntries := lc.Recent("worker", 10)
	if len(workerEntries) != 1 || workerEntries[0].Message != "hello from worker" {
		t.Fatalf("unexpected worker entries: %+v", workerEntries)
	}
}

func TestLogCache_UnknownService(t *testing.T) {
	lc := NewLogCache(10)

	got := lc.Recent("nonexistent", 5)
	if got != nil {
		t.Fatalf("expected nil for unknown service, got %v", got)
	}
}

func TestLogCache_Services(t *testing.T) {
	lc := NewLogCache(10)

	lc.Add(LogEntry{Service: "bravo"})
	lc.Add(LogEntry{Service: "alpha"})
	lc.Add(LogEntry{Service: "charlie"})

	got := lc.Services()
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Services()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLogCache_ClearService(t *testing.T) {
	lc := NewLogCache(10)

	lc.Add(LogEntry{Service: "api", Message: "one"})
	lc.Add(LogEntry{Service: "worker", Message: "two"})

	lc.Clear("api")

	if got := lc.Recent("api", 10); len(got) != 0 {
		t.Fatalf("expected empty after Clear, got %+v", got)
	}
	if got := lc.Recent("worker", 10); len(got) != 1 {
		t.Fatalf("worker should be unaffected, got %+v", got)
	}

	// Service should still be listed (buffer exists, just empty).
	if svcs := lc.Services(); len(svcs) != 2 {
		t.Fatalf("expected 2 services after Clear, got %v", svcs)
	}
}

func TestLogCache_ClearAll(t *testing.T) {
	lc := NewLogCache(10)

	lc.Add(LogEntry{Service: "api"})
	lc.Add(LogEntry{Service: "worker"})

	lc.ClearAll()

	if svcs := lc.Services(); len(svcs) != 0 {
		t.Fatalf("expected no services after ClearAll, got %v", svcs)
	}
}

func TestLogCache_ChronologicalOrder(t *testing.T) {
	lc := NewLogCache(100)

	for i := int64(0); i < 10; i++ {
		lc.Add(LogEntry{
			ID:        i,
			Service:   "api",
			Timestamp: i * 1000,
			Message:   fmt.Sprintf("msg-%d", i),
		})
	}

	entries := lc.Recent("api", 5)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp <= entries[i-1].Timestamp {
			t.Fatalf("entries not in chronological order at index %d: %d <= %d",
				i, entries[i].Timestamp, entries[i-1].Timestamp)
		}
	}
}

func TestLogCache_ThreadSafety(t *testing.T) {
	lc := NewLogCache(1000)
	services := []string{"api", "worker", "scheduler", "gateway"}

	var wg sync.WaitGroup

	// Concurrent writers.
	for _, svc := range services {
		wg.Add(1)
		go func(service string) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				lc.Add(LogEntry{
					ID:      int64(i),
					Service: service,
					Message: fmt.Sprintf("%s-%d", service, i),
				})
			}
		}(svc)
	}

	// Concurrent readers.
	for _, svc := range services {
		wg.Add(1)
		go func(service string) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_ = lc.Recent(service, 10)
				_ = lc.Services()
			}
		}(svc)
	}

	wg.Wait()

	// After all goroutines finish, each service should have exactly 500 entries
	// (buffer size 1000 > 500 writes).
	gotServices := lc.Services()
	sort.Strings(gotServices)
	sort.Strings(services)
	if len(gotServices) != len(services) {
		t.Fatalf("expected %d services, got %d", len(services), len(gotServices))
	}

	for _, svc := range services {
		entries := lc.Recent(svc, 1000)
		if len(entries) != 500 {
			t.Fatalf("expected 500 entries for %s, got %d", svc, len(entries))
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertInts(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
}
