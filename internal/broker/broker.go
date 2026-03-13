package broker

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/foestauf/devlog/internal/cache"
	"github.com/foestauf/devlog/internal/ipc"
	"github.com/foestauf/devlog/internal/level"
	"github.com/foestauf/devlog/internal/protocol"
	"github.com/foestauf/devlog/internal/session"
	"github.com/foestauf/devlog/internal/storage"
)

const (
	batchSize     = 500
	flushInterval = 100 * time.Millisecond
	eventChSize   = 10000
	cacheSize     = 10000
)


// RetentionConfig controls automatic pruning of old data.
type RetentionConfig struct {
	MaxAgeDays int
	IntervalH  int
}

// Broker is the central coordinator. It listens on a Unix socket, accepts
// client connections, processes log events, and handles queries.
type Broker struct {
	socketPath  string
	pidPath     string
	dataDir     string
	store       *storage.Store
	cache       *cache.LogCache
	sessions    *session.Manager
	connTracker *connTracker
	listener    net.Listener
	eventCh     chan *protocol.LogMessage
	done        chan struct{}
	quit        chan struct{}
	connWg      sync.WaitGroup // tracks acceptLoop + handleConnection goroutines
	writerWg    sync.WaitGroup // tracks batchWriter goroutine
	flockFile   *os.File
	startedAt   time.Time
	retention   RetentionConfig
}

// Option configures broker behaviour.
type Option func(*Broker)

// WithRetention sets the retention policy for automatic pruning.
func WithRetention(maxAgeDays, intervalH int) Option {
	return func(b *Broker) {
		b.retention = RetentionConfig{MaxAgeDays: maxAgeDays, IntervalH: intervalH}
	}
}

// New initialises a Broker with storage, cache, session manager, and event
// channel. It does not start listening — call Start for that.
func New(dataDir string, opts ...Option) (*Broker, error) {
	dbPath := filepath.Join(dataDir, "devlog.db")
	store, err := storage.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("broker: open storage: %w", err)
	}

	b := &Broker{
		socketPath:  filepath.Join(dataDir, "devlog.sock"),
		pidPath:     filepath.Join(dataDir, "devlog.pid"),
		dataDir:     dataDir,
		store:       store,
		cache:       cache.NewLogCache(cacheSize),
		sessions:    session.NewManager(),
		connTracker: newConnTracker(),
		eventCh:     make(chan *protocol.LogMessage, eventChSize),
		done:        make(chan struct{}),
		quit:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b, nil
}

// SocketPath returns the path to the Unix socket.
func (b *Broker) SocketPath() string {
	return b.socketPath
}

// Start creates the Unix socket, writes a PID file, warms the cache from DB,
// and kicks off the accept loop, batch writer, and signal handler as
// background goroutines.
func (b *Broker) Start() error {
	// Acquire flock to enforce broker singleton.
	flockFile, err := acquireFlock(b.pidPath)
	if err != nil {
		return err
	}
	b.flockFile = flockFile

	// Write PID into the lock file.
	pid := os.Getpid()
	if err := b.flockFile.Truncate(0); err != nil {
		releaseFlock(b.flockFile)
		return fmt.Errorf("broker: truncate pid file: %w", err)
	}
	if _, err := b.flockFile.Seek(0, 0); err != nil {
		releaseFlock(b.flockFile)
		return fmt.Errorf("broker: seek pid file: %w", err)
	}
	if _, err := fmt.Fprintf(b.flockFile, "%d", pid); err != nil {
		releaseFlock(b.flockFile)
		return fmt.Errorf("broker: write pid: %w", err)
	}

	ln, err := ipc.Listen(b.socketPath)
	if err != nil {
		releaseFlock(b.flockFile)
		return fmt.Errorf("broker: listen: %w", err)
	}
	b.listener = ln
	b.startedAt = time.Now()

	// Warm cache from DB before accepting connections.
	b.warmCache()

	b.connWg.Add(1)
	go b.acceptLoop()

	b.writerWg.Add(1)
	go b.batchWriter()

	go b.signalHandler()

	if b.retention.MaxAgeDays > 0 && b.retention.IntervalH > 0 {
		go b.retentionLoop()
	}

	return nil
}

// Stop performs a graceful shutdown: closes the listener, waits for all
// connection handlers and the batch writer to drain, closes DB, and cleans
// up socket, PID file, and flock.
func (b *Broker) Stop() error {
	// Signal everything to wind down.
	select {
	case <-b.done:
		// Already closed, nothing to do.
		return nil
	default:
		close(b.done)
	}

	// Close listener so acceptLoop unblocks.
	if b.listener != nil {
		b.listener.Close()
	}

	// Phase 1: Wait for acceptLoop and all handleConnection goroutines to
	// finish. After this, no more events will be pushed to eventCh.
	b.connWg.Wait()

	// Phase 2: Close the event channel so batchWriter drains and exits.
	close(b.eventCh)
	b.writerWg.Wait()

	// Close the store.
	if err := b.store.Close(); err != nil {
		return fmt.Errorf("broker: close store: %w", err)
	}

	// Clean up socket and PID file.
	ipc.Cleanup(b.socketPath)
	releaseFlock(b.flockFile)
	os.Remove(b.pidPath)

	// Signal Wait() callers.
	close(b.quit)

	return nil
}

// Wait blocks until the broker has fully stopped.
func (b *Broker) Wait() {
	<-b.quit
}

// acceptLoop runs in its own goroutine, accepting connections until the
// listener is closed.
func (b *Broker) acceptLoop() {
	defer b.connWg.Done()

	for {
		conn, err := b.listener.Accept()
		if err != nil {
			select {
			case <-b.done:
				return
			default:
				log.Printf("broker: accept error: %v", err)
				continue
			}
		}
		b.connWg.Add(1)
		go func() {
			defer b.connWg.Done()
			b.handleConnection(conn)
		}()
	}
}

// handleConnection reads messages from a single client connection and
// dispatches them based on type. On disconnect, any sessions not cleanly
// ended are marked as ended in the DB.
func (b *Broker) handleConnection(conn net.Conn) {
	defer conn.Close()
	defer b.cleanupConnection(conn)

	dec := protocol.NewDecoder(conn)
	enc := protocol.NewEncoder(conn)

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				return
			}
			select {
			case <-b.done:
				return
			default:
				log.Printf("broker: decode error: %v", err)
				return
			}
		}

		msg, err := protocol.DecodeMessage(raw)
		if err != nil {
			log.Printf("broker: decode message: %v", err)
			continue
		}

		switch m := msg.(type) {
		case *protocol.SessionStartMessage:
			b.handleSessionStart(m, enc, conn)
		case *protocol.LogMessage:
			b.handleLog(m)
		case *protocol.SessionEndMessage:
			b.handleSessionEnd(m, conn)
		case *protocol.QueryMessage:
			b.handleQuery(m, enc)
		default:
			log.Printf("broker: unexpected message type: %T", m)
		}
	}
}

// cleanupConnection marks any sessions still owned by this connection as
// ended. Called when a client disconnects without sending session_end.
func (b *Broker) cleanupConnection(conn net.Conn) {
	orphanedIDs := b.connTracker.deregister(conn)
	if len(orphanedIDs) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	for _, id := range orphanedIDs {
		if err := b.store.EndSession(id, now); err != nil {
			log.Printf("broker: cleanup session %d: %v", id, err)
		}
		b.sessions.Deregister(id)
	}
	log.Printf("broker: cleaned up %d orphaned session(s) from dropped connection", len(orphanedIDs))
}

// handleSessionStart creates a session in the DB, registers it with the
// session manager and connection tracker, and sends the session ID back to
// the client.
func (b *Broker) handleSessionStart(msg *protocol.SessionStartMessage, enc *protocol.Encoder, conn net.Conn) {
	hostname, _ := os.Hostname()
	id, err := b.store.CreateSession(msg.Service, msg.PID, msg.Command, hostname)
	if err != nil {
		log.Printf("broker: create session: %v", err)
		return
	}

	b.sessions.Register(&session.Session{
		ID:        id,
		Service:   msg.Service,
		PID:       msg.PID,
		Command:   msg.Command,
		StartedAt: msg.Timestamp,
		Hostname:  hostname,
	})
	b.connTracker.register(conn, id)

	resp := protocol.SessionStartResponse{
		Type:      protocol.TypeSessionStartReply,
		SessionID: strconv.FormatInt(id, 10),
	}
	if err := enc.Encode(resp); err != nil {
		log.Printf("broker: send session response: %v", err)
	}
}

// handleLog adds the log entry to the in-memory cache immediately, then
// pushes it onto the event channel for the batch writer. The client is
// never blocked on a DB write.
func (b *Broker) handleLog(msg *protocol.LogMessage) {
	sessID, _ := strconv.ParseInt(msg.SessionID, 10, 64)

	// Detect log level from message content.
	detectedLevel := level.Detect(msg.Message)
	if detectedLevel == "" && msg.Stream == "stderr" {
		detectedLevel = "error"
	}

	b.cache.Add(cache.LogEntry{
		SessionID: sessID,
		Timestamp: msg.Timestamp,
		Stream:    msg.Stream,
		Level:     detectedLevel,
		Message:   msg.Message,
		Service:   msg.Service,
	})

	select {
	case b.eventCh <- msg:
	default:
		// Channel full — drop the event rather than blocking the client.
		log.Printf("broker: event channel full, dropping log event")
	}
}

// handleSessionEnd marks the session as ended in the DB and deregisters it
// from the session manager and connection tracker.
func (b *Broker) handleSessionEnd(msg *protocol.SessionEndMessage, conn net.Conn) {
	sessID, err := strconv.ParseInt(msg.SessionID, 10, 64)
	if err != nil {
		log.Printf("broker: parse session id %q: %v", msg.SessionID, err)
		return
	}

	if err := b.store.EndSession(sessID, msg.Timestamp); err != nil {
		log.Printf("broker: end session: %v", err)
	}
	b.sessions.Deregister(sessID)
	b.connTracker.unregisterSession(conn, sessID)
}

// handleQuery routes queries to the appropriate backend and sends the
// response back over the connection.
func (b *Broker) handleQuery(msg *protocol.QueryMessage, enc *protocol.Encoder) {
	var resp protocol.QueryResponse
	resp.Type = protocol.TypeResult

	switch msg.Command {
	case "recent":
		limit := msg.Limit
		if limit <= 0 {
			limit = 100
		}
		entries := b.cache.Recent(msg.Service, limit)
		resp.Events = cacheEntriesToEvents(entries)

	case "search":
		var since int64
		if msg.Since != "" {
			since, _ = strconv.ParseInt(msg.Since, 10, 64)
		}
		events, err := b.store.SearchLogs(msg.Service, msg.Query, since)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Events = storageEventsToProtocol(events)
		}

	case "errors":
		var since int64
		if msg.Since != "" {
			since, _ = strconv.ParseInt(msg.Since, 10, 64)
		}
		events, err := b.store.QueryErrors(msg.Service, since)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Events = storageEventsToProtocol(events)
		}

	case "sessions":
		sessions, err := b.store.QuerySessions(msg.Service)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Sessions = storageSessionsToProtocol(sessions)
		}

	case "services":
		services, err := b.store.QueryServices()
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Services = services
		}

	case "status":
		b.handleStatus(enc)
		return

	default:
		resp.Error = fmt.Sprintf("unknown query command: %q", msg.Command)
	}

	if err := enc.Encode(resp); err != nil {
		log.Printf("broker: send query response: %v", err)
	}
}

// handleStatus builds and sends a StatusResponse with broker health info.
func (b *Broker) handleStatus(enc *protocol.Encoder) {
	services := b.cache.Services()
	cacheEntries := make(map[string]int, len(services))
	for _, svc := range services {
		cacheEntries[svc] = b.cache.EntryCount(svc)
	}

	resp := protocol.StatusResponse{
		Type:           protocol.TypeStatusReply,
		PID:            os.Getpid(),
		UptimeSeconds:  time.Since(b.startedAt).Seconds(),
		ActiveSessions: b.sessions.Count(),
		QueueDepth:     len(b.eventCh),
		Services:       services,
		CacheEntries:   cacheEntries,
	}
	if err := enc.Encode(resp); err != nil {
		log.Printf("broker: send status response: %v", err)
	}
}

// warmCache populates the in-memory cache from SQLite on startup.
func (b *Broker) warmCache() {
	services, err := b.store.QueryServices()
	if err != nil {
		log.Printf("broker: warm cache: query services: %v", err)
		return
	}

	total := 0
	for _, svc := range services {
		events, err := b.store.QueryRecentLogs(svc, cacheSize)
		if err != nil {
			log.Printf("broker: warm cache: query %s: %v", svc, err)
			continue
		}
		// QueryRecentLogs returns DESC (newest first); insert in reverse
		// so the ring buffer ends up in chronological order.
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			b.cache.Add(cache.LogEntry{
				SessionID: e.SessionID,
				Timestamp: e.Timestamp,
				Stream:    e.Stream,
				Level:     e.Level,
				Message:   e.Message,
				Service:   e.Service,
			})
		}
		total += len(events)
	}
	if total > 0 {
		log.Printf("broker: cache warmed with %d events across %d services", total, len(services))
	}
}

// batchWriter drains the event channel and flushes accumulated log events to
// the DB in batches. It flushes when the batch hits batchSize events or after
// flushInterval, whichever comes first. It exits when eventCh is closed
// (which happens after all connection handlers have finished).
func (b *Broker) batchWriter() {
	defer b.writerWg.Done()

	batch := make([]storage.LogEvent, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := b.store.InsertLogEvents(batch); err != nil {
			log.Printf("broker: flush batch: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case msg, ok := <-b.eventCh:
			if !ok {
				// Channel closed — all handlers are done. Final flush.
				flush()
				return
			}
			sessID, _ := strconv.ParseInt(msg.SessionID, 10, 64)
			detectedLevel := level.Detect(msg.Message)
			if detectedLevel == "" && msg.Stream == "stderr" {
				detectedLevel = "error"
			}
			batch = append(batch, storage.LogEvent{
				SessionID: sessID,
				Timestamp: msg.Timestamp,
				Stream:    msg.Stream,
				Level:     detectedLevel,
				Message:   msg.Message,
				Service:   msg.Service,
			})
			if len(batch) >= batchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

// signalHandler listens for SIGINT and SIGTERM and triggers a graceful
// shutdown.
func (b *Broker) signalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		log.Println("broker: received shutdown signal")
		b.Stop()
	case <-b.done:
	}
}

// retentionLoop periodically prunes old events and sessions.
func (b *Broker) retentionLoop() {
	interval := time.Duration(b.retention.IntervalH) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			deleted, err := b.store.PruneByAge(b.retention.MaxAgeDays)
			if err != nil {
				log.Printf("broker: retention prune: %v", err)
				continue
			}
			if deleted > 0 {
				log.Printf("broker: pruned %d old entries", deleted)
				if err := b.store.Checkpoint(); err != nil {
					log.Printf("broker: WAL checkpoint: %v", err)
				}
			}
		case <-b.done:
			return
		}
	}
}

// cacheEntriesToEvents converts cache entries to protocol log events.
func cacheEntriesToEvents(entries []cache.LogEntry) []protocol.LogEvent {
	events := make([]protocol.LogEvent, len(entries))
	for i, e := range entries {
		events[i] = protocol.LogEvent{
			ID:        e.ID,
			SessionID: strconv.FormatInt(e.SessionID, 10),
			Timestamp: e.Timestamp,
			Stream:    e.Stream,
			Level:     e.Level,
			Message:   e.Message,
			Service:   e.Service,
		}
	}
	return events
}

// storageEventsToProtocol converts storage log events to protocol log events.
func storageEventsToProtocol(events []storage.LogEvent) []protocol.LogEvent {
	result := make([]protocol.LogEvent, len(events))
	for i, e := range events {
		result[i] = protocol.LogEvent{
			ID:        e.ID,
			SessionID: strconv.FormatInt(e.SessionID, 10),
			Timestamp: e.Timestamp,
			Stream:    e.Stream,
			Level:     e.Level,
			Message:   e.Message,
			Service:   e.Service,
		}
	}
	return result
}

// storageSessionsToProtocol converts storage sessions to protocol sessions.
func storageSessionsToProtocol(sessions []storage.SessionInfo) []protocol.SessionInfo {
	result := make([]protocol.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = protocol.SessionInfo{
			ID:        strconv.FormatInt(s.ID, 10),
			Service:   s.Service,
			PID:       s.PID,
			Command:   s.Command,
			StartedAt: s.StartedAt,
			EndedAt:   s.EndedAt,
		}
	}
	return result
}
