# DevLog Architecture

Detailed technical architecture for the DevLog system.

---

## System Overview

```
┌──────────────────────────────────────────────────────────┐
│                     Developer Machine                     │
│                                                          │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐                 │
│  │Client: A│  │Client: B│  │Client: C│   (N clients)   │
│  │ "api"   │  │"worker" │  │ "web"   │                  │
│  └────┬────┘  └────┬────┘  └────┬────┘                  │
│       │            │            │                        │
│       └────────────┼────────────┘                        │
│                    │  Unix Socket                        │
│                    ▼                                     │
│  ┌─────────────────────────────────────┐                │
│  │              Broker                  │                │
│  │                                     │                │
│  │  ┌───────────┐  ┌───────────────┐  │                │
│  │  │ Connection │  │  Event Queue  │  │                │
│  │  │  Manager   │  │  (channel)    │  │                │
│  │  └───────────┘  └───────┬───────┘  │                │
│  │                         │          │                 │
│  │              ┌──────────┼────────┐ │                 │
│  │              ▼          ▼        │ │                 │
│  │  ┌──────────────┐ ┌──────────┐  │ │                 │
│  │  │ Memory Cache  │ │  Batch   │  │ │                 │
│  │  │ (ring buffers)│ │  Writer  │  │ │                 │
│  │  └──────────────┘ └────┬─────┘  │ │                 │
│  │                        ▼        │ │                 │
│  │                   ┌─────────┐   │ │                 │
│  │                   │ SQLite  │   │ │                 │
│  │                   │  (WAL)  │   │ │                 │
│  │                   └─────────┘   │ │                 │
│  │                                 │ │                 │
│  │  ┌───────────┐  ┌───────────┐  │ │                 │
│  │  │   Query   │  │    MCP    │  │ │                 │
│  │  │  Handler  │  │  Server   │  │ │                 │
│  │  └───────────┘  └───────────┘  │ │                 │
│  └─────────────────────────────────┘ │                 │
└──────────────────────────────────────────────────────────┘
```

---

## Component Details

### Client (Process Wrapper)

The client is invoked via `devlog run --service <name> -- <command>`. It does three things simultaneously:

1. **Spawns the child process** — executes the given command, inheriting the terminal environment
2. **Captures output** — reads stdout/stderr line-by-line via pipes
3. **Streams to broker** — sends JSON-encoded log events over the Unix socket

#### Transparent passthrough

The client must mirror all output to the terminal exactly as-is. From the developer's perspective, `devlog run --service api -- pnpm dev` must look and feel identical to `pnpm dev`. No extra prefixes, no buffering delays, no swallowed output.

#### Broker lifecycle management

On startup, the client:

1. Checks for broker socket at `~/.devlog/devlog.sock`
2. If missing, forks the broker as a background process
3. Connects to the socket
4. Sends a session registration message
5. Begins streaming log events

If the broker dies mid-stream, the client detects the broken pipe and enters reconnect mode — periodically attempting to restart the broker and resume streaming.

#### Signal handling

The client forwards signals (SIGINT, SIGTERM) to the child process and exits with the child's exit code.

---

### Broker (Coordinator)

The broker is the central process. It runs as a background daemon, started automatically by the first client that needs it.

#### Connection handling

- Listens on Unix socket at `~/.devlog/devlog.sock`
- Accepts multiple concurrent client connections
- Each connection handled in its own goroutine
- Tracks active sessions per connection

#### Event pipeline

```
Client connection
  → JSON decode
  → Event queue (Go channel, buffered)
  → Fan-out:
      → Memory cache (immediate)
      → Batch writer (accumulate → flush)
```

The event queue decouples client writes from database writes. Clients never block on SQLite.

#### Batch writer

Events accumulate in a buffer and flush to SQLite when either:
- Buffer reaches N events (e.g., 500)
- Timer fires (e.g., every 100ms)
- Whichever comes first

This amortizes the cost of SQLite transactions across many events.

#### Query routing

Queries hit different backends depending on type:

| Query | Backend |
|-------|---------|
| Recent logs | Memory cache (ring buffer) |
| Search | SQLite (LIKE / pattern match) |
| Errors | SQLite (filtered by level) |
| Sessions | SQLite |
| Services | Memory cache or SQLite |

---

### Storage (SQLite)

Database lives at `~/.devlog/devlog.db`.

#### Configuration

- WAL mode enabled (concurrent reads during writes)
- Single writer (only the broker writes)
- Synchronous = NORMAL (safe with WAL, better performance than FULL)
- Journal size limit configured to prevent unbounded WAL growth

#### Schema

```sql
CREATE TABLE sessions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    service     TEXT NOT NULL,
    pid         INTEGER NOT NULL,
    command     TEXT NOT NULL,
    started_at  INTEGER NOT NULL,  -- unix timestamp
    ended_at    INTEGER,           -- null while active
    hostname    TEXT
);

CREATE TABLE log_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  INTEGER NOT NULL REFERENCES sessions(id),
    timestamp   INTEGER NOT NULL,  -- unix timestamp (ms)
    stream      TEXT NOT NULL,     -- 'stdout' or 'stderr'
    level       TEXT,              -- nullable, detected heuristically
    message     TEXT NOT NULL,
    service     TEXT NOT NULL      -- denormalized for query performance
);

CREATE INDEX idx_log_events_service_ts ON log_events(service, timestamp);
CREATE INDEX idx_log_events_session ON log_events(session_id);
CREATE INDEX idx_sessions_service ON sessions(service);
```

The `service` column is denormalized onto `log_events` to avoid joins on the hot query path.

---

### Memory Cache

Per-service ring buffers holding the most recent N log lines (default 10,000).

#### Properties

- **Not source of truth** — rebuilt from SQLite on broker restart
- **Write path** — every event written to cache immediately (before DB batch)
- **Read path** — `recent` queries served entirely from cache
- **Eviction** — oldest entries drop off when buffer is full
- **Thread safety** — protected by read-write mutex per buffer

---

### MCP Server

Runs as a subprocess of the broker (or standalone), communicating via stdio JSON-RPC.

#### Tools

**get_recent_logs**
```json
{
  "service": "api",
  "limit": 100
}
```
Returns the N most recent log lines for the service. Served from memory cache.

**search_logs**
```json
{
  "service": "api",
  "query": "timeout",
  "since": "2024-03-12T00:00:00Z"
}
```
Searches log messages by substring. Hits SQLite.

**get_errors**
```json
{
  "service": "api",
  "since": "2024-03-12T10:00:00Z"
}
```
Returns stderr lines and any lines with detected error-level keywords.

**get_sessions**
```json
{
  "service": "api"
}
```
Lists sessions with start/end times, PIDs, and commands.

**get_service_list**
```json
{}
```
Returns all known service names.

---

## IPC Protocol

Communication between clients and broker uses newline-delimited JSON over Unix sockets.

### Message Types

**Session registration (client → broker):**
```json
{
  "type": "session_start",
  "service": "api",
  "pid": 12345,
  "command": "pnpm dev",
  "timestamp": 1710253200
}
```

**Log event (client → broker):**
```json
{
  "type": "log",
  "service": "api",
  "session": "session_12",
  "timestamp": 1710253200123,
  "stream": "stdout",
  "message": "Server started on :3000"
}
```

**Session end (client → broker):**
```json
{
  "type": "session_end",
  "session": "session_12",
  "timestamp": 1710253260,
  "exit_code": 0
}
```

**Query request (CLI → broker):**
```json
{
  "type": "query",
  "command": "recent",
  "service": "api",
  "limit": 200
}
```

**Query response (broker → CLI):**
```json
{
  "type": "result",
  "events": [...]
}
```

---

## File Locations

| Path | Purpose |
|------|---------|
| `~/.devlog/devlog.db` | SQLite database |
| `~/.devlog/devlog.sock` | Unix domain socket |
| `~/.devlog/devlog.pid` | Broker PID file |
| `~/.devlog/config.yaml` | Optional configuration (future) |

---

## Concurrency Model

The broker uses goroutines extensively:

- **1 goroutine** per client connection (read loop)
- **1 goroutine** for the batch writer (flush loop)
- **1 goroutine** for the socket listener (accept loop)
- **N goroutines** for query handling (per-request)

Channels connect the pieces:
- Client read goroutines push events into a shared buffered channel
- Batch writer drains the channel and flushes to SQLite
- Memory cache updates happen synchronously in the client read goroutine (fast, lock-protected)

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Broker crashes | Clients detect broken pipe, enter reconnect loop, restart broker |
| Client crashes | Broker detects EOF, closes session |
| SQLite corruption | Broker logs error, attempts recovery or re-creates DB |
| Socket already exists | Check PID file, remove stale socket if broker not running |
| Disk full | Broker logs warning, continues serving from cache |
