package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/foestauf/devlog/internal/ipc"
	"github.com/foestauf/devlog/internal/protocol"
)

// Client handles communication with the broker over a Unix domain socket.
// It supports auto-reconnection with client-side buffering during broker
// downtime.
type Client struct {
	service    string
	socketPath string
	dataDir    string
	conn       net.Conn
	encoder    *protocol.Encoder
	decoder    *protocol.Decoder
	sessionID  string

	// Stored for re-registration after reconnect.
	pid     int
	command string

	// Reconnect state.
	mu           sync.Mutex // guards conn/encoder/decoder during reconnect
	buf          *lineBuffer
	reconnecting int32 // atomic flag: 1 if reconnect in progress
}

// New creates a new Client for the given service.
func New(service string, dataDir string) *Client {
	return &Client{
		service:    service,
		socketPath: filepath.Join(dataDir, "devlog.sock"),
		dataDir:    dataDir,
		buf:        newLineBuffer(),
	}
}

// Connect dials the broker's Unix socket and initialises the encoder/decoder.
func (c *Client) Connect() error {
	conn, err := ipc.Dial(c.socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("client: connect to broker: %w", err)
	}
	c.conn = conn
	c.encoder = protocol.NewEncoder(conn)
	c.decoder = protocol.NewDecoder(conn)
	return nil
}

// RegisterSession sends a SessionStartMessage to the broker and reads back
// the assigned session ID. It also stores the pid and command for use during
// reconnection.
func (c *Client) RegisterSession(pid int, command string) (string, error) {
	c.pid = pid
	c.command = command

	msg := protocol.SessionStartMessage{
		Type:      protocol.TypeSessionStart,
		Service:   c.service,
		PID:       pid,
		Command:   command,
		Timestamp: time.Now().UnixMilli(),
	}
	if err := c.encoder.Encode(msg); err != nil {
		return "", fmt.Errorf("client: send session start: %w", err)
	}

	var resp protocol.SessionStartResponse
	if err := c.decoder.Decode(&resp); err != nil {
		return "", fmt.Errorf("client: read session start response: %w", err)
	}

	c.sessionID = resp.SessionID
	return c.sessionID, nil
}

// SendLog sends a log line to the broker. On failure, the line is buffered
// and a reconnection attempt is triggered in the background. Returns nil
// even on send failure — the line is buffered, not lost.
func (c *Client) SendLog(ctx context.Context, stream, message string, ts int64) {
	c.mu.Lock()
	msg := protocol.LogMessage{
		Type:      protocol.TypeLog,
		Service:   c.service,
		SessionID: c.sessionID,
		Timestamp: ts,
		Stream:    stream,
		Message:   message,
	}
	err := c.encoder.Encode(msg)
	c.mu.Unlock()

	if err == nil {
		return
	}

	// Encode failed — buffer the line and trigger reconnect.
	c.buf.push(bufferedLine{
		Stream:    stream,
		Message:   message,
		Timestamp: ts,
	})

	if atomic.CompareAndSwapInt32(&c.reconnecting, 0, 1) {
		go c.reconnectLoop(ctx)
	}
}

// reconnectLoop attempts to reconnect to the broker with a polling backoff.
// On success, it flushes all buffered lines.
func (c *Client) reconnectLoop(ctx context.Context) {
	defer atomic.StoreInt32(&c.reconnecting, 0)

	fmt.Fprintf(os.Stderr, "devlog: broker connection lost, reconnecting...\n")

	for {
		if ctx.Err() != nil {
			return
		}

		if err := c.reconnect(); err == nil {
			// Flush buffered lines.
			c.flushBuffer()
			fmt.Fprintf(os.Stderr, "devlog: reconnected to broker\n")
			return
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// reconnect closes the old connection, ensures the broker is running,
// dials a new connection, and re-registers the session. The mutex is only
// held during the connection swap to avoid blocking terminal output during
// the EnsureBroker polling period.
func (c *Client) reconnect() error {
	// Close old connection (under lock).
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()

	// Ensure broker is running (no lock — this can take up to 2s).
	if err := EnsureBroker(c.dataDir); err != nil {
		return err
	}

	// Dial new connection (no lock — blocking I/O).
	conn, err := ipc.Dial(c.socketPath, 5*time.Second)
	if err != nil {
		return err
	}

	enc := protocol.NewEncoder(conn)
	dec := protocol.NewDecoder(conn)

	// Re-register session before swapping (no lock needed yet — nobody
	// else uses this conn/encoder/decoder).
	var newSessionID string
	if c.pid != 0 {
		msg := protocol.SessionStartMessage{
			Type:      protocol.TypeSessionStart,
			Service:   c.service,
			PID:       c.pid,
			Command:   c.command,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := enc.Encode(msg); err != nil {
			conn.Close()
			return fmt.Errorf("client: re-register session: %w", err)
		}

		var resp protocol.SessionStartResponse
		if err := dec.Decode(&resp); err != nil {
			conn.Close()
			return fmt.Errorf("client: read session re-register response: %w", err)
		}
		newSessionID = resp.SessionID
	}

	// Swap in the new connection (under lock).
	c.mu.Lock()
	c.conn = conn
	c.encoder = enc
	c.decoder = dec
	if newSessionID != "" {
		c.sessionID = newSessionID
	}
	c.mu.Unlock()

	return nil
}

// flushBuffer sends all buffered lines to the broker.
func (c *Client) flushBuffer() {
	lines := c.buf.drain()
	if len(lines) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, line := range lines {
		msg := protocol.LogMessage{
			Type:      protocol.TypeLog,
			Service:   c.service,
			SessionID: c.sessionID,
			Timestamp: line.Timestamp,
			Stream:    line.Stream,
			Message:   line.Message,
		}
		// Best-effort — if this fails during flush, we drop these lines
		// rather than re-buffering (which could loop forever).
		if err := c.encoder.Encode(msg); err != nil {
			return
		}
	}
}

// EndSession sends a SessionEndMessage to the broker.
func (c *Client) EndSession(exitCode int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	msg := protocol.SessionEndMessage{
		Type:      protocol.TypeSessionEnd,
		SessionID: c.sessionID,
		Timestamp: time.Now().UnixMilli(),
		ExitCode:  exitCode,
	}
	if err := c.encoder.Encode(msg); err != nil {
		return fmt.Errorf("client: send session end: %w", err)
	}
	return nil
}

// Close shuts down the connection to the broker.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
