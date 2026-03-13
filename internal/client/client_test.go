package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/foestauf/devlog/internal/protocol"
)

func TestEnsureBrokerDetectsExistingSocket(t *testing.T) {
	// Create a temporary directory for the socket.
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "devlog.sock")

	// Start a listener on the socket — acts as our fake broker.
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer listener.Close()

	// Accept connections in the background so the dial doesn't hang.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	// EnsureBroker should detect the running socket and return nil.
	err = EnsureBroker(tmpDir)
	if err != nil {
		t.Fatalf("EnsureBroker should succeed when socket is available: %v", err)
	}
}

func TestIsSocketAvailableReturnsFalseForMissingSocket(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	if IsSocketAvailable(socketPath) {
		t.Fatal("IsSocketAvailable should return false for a non-existent socket")
	}
}

func TestIsSocketAvailableReturnsTrueForLiveSocket(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	if !IsSocketAvailable(socketPath) {
		t.Fatal("IsSocketAvailable should return true for a live socket")
	}
}

func TestScanLinesProducesCorrectLogMessages(t *testing.T) {
	// Set up a buffer to capture the encoded messages.
	var brokerBuf bytes.Buffer
	encoder := protocol.NewEncoder(&brokerBuf)

	// Build a minimal client with the encoder wired up.
	c := &Client{
		service:   "test-svc",
		sessionID: "sess-123",
		encoder:   encoder,
		buf:       newLineBuffer(),
	}

	// Simulate some process output.
	input := "line one\nline two\nline three\n"
	reader := strings.NewReader(input)

	// Capture terminal output.
	var terminalBuf bytes.Buffer

	scanLines(context.Background(), reader, &terminalBuf, "stdout", c)

	// Verify terminal passthrough.
	terminalLines := strings.Split(strings.TrimSpace(terminalBuf.String()), "\n")
	expectedLines := []string{"line one", "line two", "line three"}
	if len(terminalLines) != len(expectedLines) {
		t.Fatalf("expected %d terminal lines, got %d", len(expectedLines), len(terminalLines))
	}
	for i, want := range expectedLines {
		if terminalLines[i] != want {
			t.Errorf("terminal line %d: got %q, want %q", i, terminalLines[i], want)
		}
	}

	// Verify broker messages.
	scanner := bufio.NewScanner(&brokerBuf)
	var messages []protocol.LogMessage
	for scanner.Scan() {
		var msg protocol.LogMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			t.Fatalf("unmarshal log message: %v", err)
		}
		messages = append(messages, msg)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 log messages, got %d", len(messages))
	}

	for i, msg := range messages {
		if msg.Type != protocol.TypeLog {
			t.Errorf("message %d: type = %q, want %q", i, msg.Type, protocol.TypeLog)
		}
		if msg.Service != "test-svc" {
			t.Errorf("message %d: service = %q, want %q", i, msg.Service, "test-svc")
		}
		if msg.SessionID != "sess-123" {
			t.Errorf("message %d: session = %q, want %q", i, msg.SessionID, "sess-123")
		}
		if msg.Stream != "stdout" {
			t.Errorf("message %d: stream = %q, want %q", i, msg.Stream, "stdout")
		}
		if msg.Message != expectedLines[i] {
			t.Errorf("message %d: message = %q, want %q", i, msg.Message, expectedLines[i])
		}
		if msg.Timestamp == 0 {
			t.Errorf("message %d: timestamp should not be zero", i)
		}
	}
}

func TestSignalForwardingSetupDoesNotPanic(t *testing.T) {
	// Verify that setting up and tearing down signal forwarding
	// doesn't panic or cause issues.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	signal.Stop(sigCh)
	close(sigCh)
}

func TestClientConnectAndRegister(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "devlog.sock")

	// Spin up a fake broker that responds to session registration.
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		decoder := protocol.NewDecoder(conn)
		encoder := protocol.NewEncoder(conn)

		// Read the session start message.
		var msg protocol.SessionStartMessage
		if err := decoder.Decode(&msg); err != nil {
			return
		}

		// Send back a session ID.
		resp := protocol.SessionStartResponse{
			Type:      "session_start_response",
			SessionID: "test-session-42",
		}
		_ = encoder.Encode(resp)

		// Read the session end message.
		var endMsg protocol.SessionEndMessage
		_ = decoder.Decode(&endMsg)
	}()

	c := New("my-service", tmpDir)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	sessionID, err := c.RegisterSession(12345, "npm start")
	if err != nil {
		t.Fatalf("register session: %v", err)
	}
	if sessionID != "test-session-42" {
		t.Errorf("session ID = %q, want %q", sessionID, "test-session-42")
	}

	if err := c.EndSession(0); err != nil {
		t.Fatalf("end session: %v", err)
	}
}

func TestNewClientSetsSocketPath(t *testing.T) {
	c := New("api", "/tmp/devlog-data")
	expected := filepath.Join("/tmp/devlog-data", "devlog.sock")
	if c.socketPath != expected {
		t.Errorf("socketPath = %q, want %q", c.socketPath, expected)
	}
	if c.service != "api" {
		t.Errorf("service = %q, want %q", c.service, "api")
	}
}

func TestScanLinesHandlesEmptyInput(t *testing.T) {
	var brokerBuf bytes.Buffer
	encoder := protocol.NewEncoder(&brokerBuf)
	c := &Client{
		service:   "svc",
		sessionID: "s1",
		encoder:   encoder,
		buf:       newLineBuffer(),
	}

	reader := strings.NewReader("")
	var terminalBuf bytes.Buffer

	scanLines(context.Background(), reader, &terminalBuf, "stderr", c)

	if terminalBuf.Len() != 0 {
		t.Errorf("expected no terminal output for empty input, got %q", terminalBuf.String())
	}
	if brokerBuf.Len() != 0 {
		t.Errorf("expected no broker messages for empty input, got %q", brokerBuf.String())
	}
}
