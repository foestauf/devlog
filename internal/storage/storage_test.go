package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewCreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer store.Close()

	// File should exist on disk.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestTablesExist(t *testing.T) {
	store := newTestStore(t)

	tables := []string{"sessions", "log_events"}
	for _, table := range tables {
		var name string
		err := store.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}
}

func TestCreateAndEndSession(t *testing.T) {
	store := newTestStore(t)

	id, err := store.CreateSession("web-api", 1234, "go run ./cmd/api", "localhost")
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	if id < 1 {
		t.Fatalf("expected positive session ID, got %d", id)
	}

	endedAt := int64(1700000000000)
	if err := store.EndSession(id, endedAt); err != nil {
		t.Fatalf("EndSession error: %v", err)
	}

	sessions, err := store.QuerySessions("web-api")
	if err != nil {
		t.Fatalf("QuerySessions error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.ID != id {
		t.Errorf("session ID = %d, want %d", s.ID, id)
	}
	if s.Service != "web-api" {
		t.Errorf("service = %q, want %q", s.Service, "web-api")
	}
	if s.PID != 1234 {
		t.Errorf("PID = %d, want %d", s.PID, 1234)
	}
	if s.EndedAt == nil || *s.EndedAt != endedAt {
		t.Errorf("EndedAt = %v, want %d", s.EndedAt, endedAt)
	}
	if s.Hostname != "localhost" {
		t.Errorf("hostname = %q, want %q", s.Hostname, "localhost")
	}
}

func TestQuerySessionsAll(t *testing.T) {
	store := newTestStore(t)

	store.CreateSession("svc-a", 1, "cmd-a", "host-a")
	store.CreateSession("svc-b", 2, "cmd-b", "host-b")

	sessions, err := store.QuerySessions("")
	if err != nil {
		t.Fatalf("QuerySessions('') error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestInsertAndQueryRecentLogs(t *testing.T) {
	store := newTestStore(t)

	sessionID, _ := store.CreateSession("web-api", 100, "go run .", "localhost")

	events := []LogEvent{
		{SessionID: sessionID, Timestamp: 1000, Stream: "stdout", Level: "info", Message: "first line", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 2000, Stream: "stdout", Level: "info", Message: "second line", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 3000, Stream: "stderr", Level: "error", Message: "something broke", Service: "web-api"},
	}

	if err := store.InsertLogEvents(events); err != nil {
		t.Fatalf("InsertLogEvents error: %v", err)
	}

	recent, err := store.QueryRecentLogs("web-api", 2)
	if err != nil {
		t.Fatalf("QueryRecentLogs error: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent logs, got %d", len(recent))
	}
	// Most recent first.
	if recent[0].Timestamp != 3000 {
		t.Errorf("first result timestamp = %d, want 3000", recent[0].Timestamp)
	}
	if recent[1].Timestamp != 2000 {
		t.Errorf("second result timestamp = %d, want 2000", recent[1].Timestamp)
	}
}

func TestInsertLogEventsEmpty(t *testing.T) {
	store := newTestStore(t)

	// Should be a no-op, not an error.
	if err := store.InsertLogEvents(nil); err != nil {
		t.Fatalf("InsertLogEvents(nil) error: %v", err)
	}
	if err := store.InsertLogEvents([]LogEvent{}); err != nil {
		t.Fatalf("InsertLogEvents([]) error: %v", err)
	}
}

func TestSearchLogs(t *testing.T) {
	store := newTestStore(t)

	sessionID, _ := store.CreateSession("web-api", 100, "go run .", "localhost")

	events := []LogEvent{
		{SessionID: sessionID, Timestamp: 1000, Stream: "stdout", Level: "info", Message: "starting server on port 8080", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 2000, Stream: "stdout", Level: "info", Message: "connected to database", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 3000, Stream: "stdout", Level: "info", Message: "server ready", Service: "web-api"},
	}
	store.InsertLogEvents(events)

	// Search without time filter.
	results, err := store.SearchLogs("web-api", "server", 0)
	if err != nil {
		t.Fatalf("SearchLogs error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 search results, got %d", len(results))
	}

	// Search with since filter.
	results, err = store.SearchLogs("web-api", "server", 2500)
	if err != nil {
		t.Fatalf("SearchLogs with since error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with since filter, got %d", len(results))
	}
	if results[0].Message != "server ready" {
		t.Errorf("unexpected message: %q", results[0].Message)
	}
}

func TestQueryErrors(t *testing.T) {
	store := newTestStore(t)

	sessionID, _ := store.CreateSession("web-api", 100, "go run .", "localhost")

	events := []LogEvent{
		{SessionID: sessionID, Timestamp: 1000, Stream: "stdout", Level: "info", Message: "all good", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 2000, Stream: "stderr", Level: "", Message: "warning on stderr", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 3000, Stream: "stdout", Level: "ERROR", Message: "error level log", Service: "web-api"},
		{SessionID: sessionID, Timestamp: 4000, Stream: "stdout", Level: "info", Message: "fine again", Service: "web-api"},
	}
	store.InsertLogEvents(events)

	errors, err := store.QueryErrors("web-api", 0)
	if err != nil {
		t.Fatalf("QueryErrors error: %v", err)
	}
	if len(errors) != 2 {
		t.Fatalf("expected 2 error results, got %d", len(errors))
	}

	// With since filter — only the ERROR level one at ts=3000.
	errors, err = store.QueryErrors("web-api", 2500)
	if err != nil {
		t.Fatalf("QueryErrors with since error: %v", err)
	}
	if len(errors) != 1 {
		t.Fatalf("expected 1 error with since filter, got %d", len(errors))
	}
}

func TestQueryServices(t *testing.T) {
	store := newTestStore(t)

	store.CreateSession("alpha", 1, "cmd", "host")
	store.CreateSession("beta", 2, "cmd", "host")
	store.CreateSession("alpha", 3, "cmd", "host") // duplicate

	services, err := store.QueryServices()
	if err != nil {
		t.Fatalf("QueryServices error: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 distinct services, got %d", len(services))
	}
	if services[0] != "alpha" || services[1] != "beta" {
		t.Errorf("unexpected services: %v", services)
	}
}

func TestEmptyDatabaseReturnsEmptySlices(t *testing.T) {
	store := newTestStore(t)

	sessions, err := store.QuerySessions("")
	if err != nil {
		t.Fatalf("QuerySessions error: %v", err)
	}
	if sessions == nil {
		t.Error("QuerySessions returned nil, want empty slice")
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}

	logs, err := store.QueryRecentLogs("nope", 10)
	if err != nil {
		t.Fatalf("QueryRecentLogs error: %v", err)
	}
	if logs == nil {
		t.Error("QueryRecentLogs returned nil, want empty slice")
	}

	searchResults, err := store.SearchLogs("nope", "whatever", 0)
	if err != nil {
		t.Fatalf("SearchLogs error: %v", err)
	}
	if searchResults == nil {
		t.Error("SearchLogs returned nil, want empty slice")
	}

	errors, err := store.QueryErrors("nope", 0)
	if err != nil {
		t.Fatalf("QueryErrors error: %v", err)
	}
	if errors == nil {
		t.Error("QueryErrors returned nil, want empty slice")
	}

	services, err := store.QueryServices()
	if err != nil {
		t.Fatalf("QueryServices error: %v", err)
	}
	if services == nil {
		t.Error("QueryServices returned nil, want empty slice")
	}
}

func TestBatchInsertLargeSet(t *testing.T) {
	store := newTestStore(t)

	sessionID, _ := store.CreateSession("batch-svc", 999, "batch-cmd", "host")

	events := make([]LogEvent, 500)
	for i := range events {
		events[i] = LogEvent{
			SessionID: sessionID,
			Timestamp: int64(i),
			Stream:    "stdout",
			Level:     "info",
			Message:   "line",
			Service:   "batch-svc",
		}
	}

	if err := store.InsertLogEvents(events); err != nil {
		t.Fatalf("batch insert error: %v", err)
	}

	recent, err := store.QueryRecentLogs("batch-svc", 1000)
	if err != nil {
		t.Fatalf("QueryRecentLogs error: %v", err)
	}
	if len(recent) != 500 {
		t.Errorf("expected 500 logs, got %d", len(recent))
	}
}
