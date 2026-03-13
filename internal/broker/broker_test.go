package broker

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foestauf/devlog/internal/protocol"
	"github.com/foestauf/devlog/internal/storage"
)

func setupBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	dir := t.TempDir()
	b, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return b, dir
}

func TestBrokerStartCreatesSocket(t *testing.T) {
	b, dir := setupBroker(t)
	defer b.Stop()

	sockPath := filepath.Join(dir, "devlog.sock")
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatalf("socket file not created at %s", sockPath)
	}

	pidPath := filepath.Join(dir, "devlog.pid")
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Fatalf("pid file not created at %s", pidPath)
	}
}

func TestBrokerStopCleansUp(t *testing.T) {
	b, dir := setupBroker(t)

	sockPath := filepath.Join(dir, "devlog.sock")
	pidPath := filepath.Join(dir, "devlog.pid")

	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatalf("socket file not removed after Stop")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file not removed after Stop")
	}
}

func dialBroker(t *testing.T, b *Broker) (net.Conn, *protocol.Encoder, *protocol.Decoder) {
	t.Helper()
	conn, err := net.Dial("unix", b.SocketPath())
	if err != nil {
		t.Fatalf("dial broker: %v", err)
	}
	return conn, protocol.NewEncoder(conn), protocol.NewDecoder(conn)
}

func TestSessionStartReturnsID(t *testing.T) {
	b, _ := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)
	defer conn.Close()

	msg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "test-svc",
		PID:       12345,
		Command:   "go run .",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(msg); err != nil {
		t.Fatalf("encode session_start: %v", err)
	}

	var resp protocol.SessionStartResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode session response: %v", err)
	}

	if resp.Type != protocol.TypeSessionStartReply {
		t.Errorf("expected type %s, got %q", protocol.TypeSessionStartReply, resp.Type)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestLogAppearsInCache(t *testing.T) {
	b, _ := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)
	defer conn.Close()

	// Start a session first.
	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "cache-svc",
		PID:       1,
		Command:   "echo hello",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode session_start: %v", err)
	}

	var resp protocol.SessionStartResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode session response: %v", err)
	}

	// Send a log message.
	logMsg := protocol.LogMessage{
		Type:      protocol.TypeLog,
		Service:   "cache-svc",
		SessionID: resp.SessionID,
		Timestamp: time.Now().UnixMilli(),
		Stream:    "stdout",
		Message:   "hello from test",
	}
	if err := enc.Encode(logMsg); err != nil {
		t.Fatalf("encode log: %v", err)
	}

	// Give the broker a moment to process.
	time.Sleep(50 * time.Millisecond)

	entries := b.cache.Recent("cache-svc", 10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(entries))
	}
	if entries[0].Message != "hello from test" {
		t.Errorf("expected message %q, got %q", "hello from test", entries[0].Message)
	}
}

func TestQueryRecentFromCache(t *testing.T) {
	b, _ := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)
	defer conn.Close()

	// Start session.
	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "query-svc",
		PID:       1,
		Command:   "test",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var sessResp protocol.SessionStartResponse
	if err := dec.Decode(&sessResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Send a log.
	logMsg := protocol.LogMessage{
		Type:      protocol.TypeLog,
		Service:   "query-svc",
		SessionID: sessResp.SessionID,
		Timestamp: time.Now().UnixMilli(),
		Stream:    "stdout",
		Message:   "query me",
	}
	if err := enc.Encode(logMsg); err != nil {
		t.Fatalf("encode log: %v", err)
	}

	// Wait for processing.
	time.Sleep(50 * time.Millisecond)

	// Send a recent query.
	queryMsg := protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "recent",
		Service: "query-svc",
		Limit:   10,
	}
	if err := enc.Encode(queryMsg); err != nil {
		t.Fatalf("encode query: %v", err)
	}

	var queryResp protocol.QueryResponse
	if err := dec.Decode(&queryResp); err != nil {
		t.Fatalf("decode query response: %v", err)
	}

	if queryResp.Error != "" {
		t.Fatalf("unexpected error: %s", queryResp.Error)
	}
	if len(queryResp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(queryResp.Events))
	}
	if queryResp.Events[0].Message != "query me" {
		t.Errorf("expected message %q, got %q", "query me", queryResp.Events[0].Message)
	}
}

func TestQuerySearchHitsDB(t *testing.T) {
	b, _ := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)
	defer conn.Close()

	// Start session.
	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "search-svc",
		PID:       1,
		Command:   "test",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var sessResp protocol.SessionStartResponse
	if err := dec.Decode(&sessResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Send a log.
	logMsg := protocol.LogMessage{
		Type:      protocol.TypeLog,
		Service:   "search-svc",
		SessionID: sessResp.SessionID,
		Timestamp: time.Now().UnixMilli(),
		Stream:    "stdout",
		Message:   "needle in haystack",
	}
	if err := enc.Encode(logMsg); err != nil {
		t.Fatalf("encode log: %v", err)
	}

	// Wait for the batch writer to flush (100ms interval + a bit of margin).
	time.Sleep(250 * time.Millisecond)

	// Search query goes to DB.
	queryMsg := protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "search",
		Service: "search-svc",
		Query:   "needle",
	}
	if err := enc.Encode(queryMsg); err != nil {
		t.Fatalf("encode query: %v", err)
	}

	var queryResp protocol.QueryResponse
	if err := dec.Decode(&queryResp); err != nil {
		t.Fatalf("decode query response: %v", err)
	}

	if queryResp.Error != "" {
		t.Fatalf("unexpected error: %s", queryResp.Error)
	}
	if len(queryResp.Events) != 1 {
		t.Fatalf("expected 1 event from DB search, got %d", len(queryResp.Events))
	}
	if queryResp.Events[0].Message != "needle in haystack" {
		t.Errorf("expected message %q, got %q", "needle in haystack", queryResp.Events[0].Message)
	}
}

func TestQueryServices(t *testing.T) {
	b, _ := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)
	defer conn.Close()

	// Create a session so there's a service.
	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "svc-list-test",
		PID:       1,
		Command:   "test",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var sessResp protocol.SessionStartResponse
	if err := dec.Decode(&sessResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Query services.
	queryMsg := protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "services",
	}
	if err := enc.Encode(queryMsg); err != nil {
		t.Fatalf("encode query: %v", err)
	}

	var queryResp protocol.QueryResponse
	if err := dec.Decode(&queryResp); err != nil {
		t.Fatalf("decode query response: %v", err)
	}

	if queryResp.Error != "" {
		t.Fatalf("unexpected error: %s", queryResp.Error)
	}

	found := false
	for _, svc := range queryResp.Services {
		if svc == "svc-list-test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find service 'svc-list-test' in %v", queryResp.Services)
	}
}

func TestSessionCleanupOnDisconnect(t *testing.T) {
	b, dir := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)

	// Start a session.
	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "crash-svc",
		PID:       999,
		Command:   "crash test",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp protocol.SessionStartResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Close connection WITHOUT sending session_end (simulates crash).
	conn.Close()

	// Give the cleanup handler a moment.
	time.Sleep(100 * time.Millisecond)

	// Verify the session was marked as ended in the DB.
	store, err := storage.New(filepath.Join(dir, "devlog.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	sessions, err := store.QuerySessions("crash-svc")
	if err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].EndedAt == nil {
		t.Error("expected session to be marked as ended after client disconnect")
	}
}

func TestCacheWarmOnRestart(t *testing.T) {
	dir := t.TempDir()

	// Create a broker, insert some data, stop it.
	b1, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b1.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	conn, enc, dec := dialBroker(t, b1)

	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "warm-svc",
		PID:       1,
		Command:   "test",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp protocol.SessionStartResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	logMsg := protocol.LogMessage{
		Type:      protocol.TypeLog,
		Service:   "warm-svc",
		SessionID: resp.SessionID,
		Timestamp: time.Now().UnixMilli(),
		Stream:    "stdout",
		Message:   "warm me up",
	}
	if err := enc.Encode(logMsg); err != nil {
		t.Fatalf("encode log: %v", err)
	}

	// Wait for batch writer to flush.
	time.Sleep(250 * time.Millisecond)
	conn.Close()
	b1.Stop()

	// Start a new broker against the same data dir.
	b2, err := New(dir)
	if err != nil {
		t.Fatalf("New b2: %v", err)
	}
	if err := b2.Start(); err != nil {
		t.Fatalf("Start b2: %v", err)
	}
	defer b2.Stop()

	// Cache should be warm — recent query should return our log.
	entries := b2.cache.Recent("warm-svc", 10)
	if len(entries) == 0 {
		t.Fatal("expected cache to be warm with entries from DB")
	}
	if entries[0].Message != "warm me up" {
		t.Errorf("expected message %q, got %q", "warm me up", entries[0].Message)
	}
}

func TestStatusQuery(t *testing.T) {
	b, _ := setupBroker(t)
	defer b.Stop()

	conn, enc, dec := dialBroker(t, b)
	defer conn.Close()

	// Create a session to have some data.
	startMsg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   "status-svc",
		PID:       1,
		Command:   "test",
		Timestamp: time.Now().UnixMilli(),
	}
	if err := enc.Encode(startMsg); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var sessResp protocol.SessionStartResponse
	if err := dec.Decode(&sessResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Send status query.
	queryMsg := protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "status",
	}
	if err := enc.Encode(queryMsg); err != nil {
		t.Fatalf("encode status query: %v", err)
	}

	// Read back the StatusResponse.
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}

	var statusResp protocol.StatusResponse
	if err := json.Unmarshal(raw, &statusResp); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}

	if statusResp.Type != protocol.TypeStatusReply {
		t.Errorf("expected type %q, got %q", protocol.TypeStatusReply, statusResp.Type)
	}
	if statusResp.PID == 0 {
		t.Error("expected non-zero PID")
	}
	if statusResp.ActiveSessions != 1 {
		t.Errorf("expected 1 active session, got %d", statusResp.ActiveSessions)
	}
}

func TestDuplicateBrokerFailsFlock(t *testing.T) {
	dir := t.TempDir()

	b1, err := New(dir)
	if err != nil {
		t.Fatalf("New b1: %v", err)
	}
	if err := b1.Start(); err != nil {
		t.Fatalf("Start b1: %v", err)
	}
	defer b1.Stop()

	// A second broker against the same dir should fail.
	b2, err := New(dir)
	if err != nil {
		t.Fatalf("New b2: %v", err)
	}
	if err := b2.Start(); err == nil {
		b2.Stop()
		t.Fatal("expected second broker to fail with flock error")
	}
}
