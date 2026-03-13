# DevLog Roadmap

Phased implementation plan. Each phase builds on the previous and produces a usable increment.

---

## Phase 1: Foundation

**Goal:** Core binary skeleton, IPC, and basic broker lifecycle.

- [ ] Project scaffolding (Go module, directory structure, Makefile)
- [ ] CLI framework setup (cobra or similar)
- [ ] Broker process — starts, creates Unix socket, accepts connections
- [ ] Client process — connects to broker socket, sends test messages
- [ ] JSON message protocol (log event struct, serialization)
- [ ] Auto-start broker from client if socket missing
- [ ] Graceful shutdown handling (SIGINT/SIGTERM)

**Deliverable:** `devlog run --service test -- echo hello` connects to broker and sends a message.

---

## Phase 2: Process Wrapping

**Goal:** Client wraps real processes and streams their output.

- [ ] Spawn child process from `devlog run` arguments
- [ ] Capture stdout/stderr line-by-line
- [ ] Mirror output to terminal (transparent passthrough)
- [ ] Tag log lines with service name, stream type, timestamp
- [ ] Stream tagged events to broker over socket
- [ ] Forward child process exit code
- [ ] Handle child process signals (SIGINT passthrough)

**Deliverable:** `devlog run --service api -- pnpm dev` works identically to running `pnpm dev` directly, with logs flowing to broker.

---

## Phase 3: Storage

**Goal:** Durable log persistence in SQLite.

- [ ] SQLite database initialization (`~/.devlog/devlog.db`)
- [ ] WAL mode configuration
- [ ] Schema creation — `sessions` and `log_events` tables
- [ ] Session lifecycle — create on client connect, close on disconnect
- [ ] Batch write pipeline (queue → batch → insert)
- [ ] Non-blocking client writes (broker queues, never blocks clients)
- [ ] Database migration strategy

**Deliverable:** Logs survive broker restarts. Sessions tracked in database.

---

## Phase 4: Memory Cache

**Goal:** Fast recent-log access without hitting SQLite.

- [ ] Per-service ring buffer implementation
- [ ] Configurable buffer size (default 10k lines)
- [ ] Populate cache on broker startup from DB
- [ ] Dual-write path (cache + DB queue)
- [ ] Cache eviction on buffer full

**Deliverable:** Recent log queries served from memory in < 10ms.

---

## Phase 5: Query CLI

**Goal:** Human-facing query commands.

- [ ] `devlog recent <service> --limit N`
- [ ] `devlog search <service> "<query>"`
- [ ] `devlog errors <service> --since <duration>`
- [ ] `devlog sessions [service]`
- [ ] `devlog services`
- [ ] Query routing — memory cache for recent, SQLite for search/historical
- [ ] Output formatting (timestamps, service labels, stream indicators)

**Deliverable:** Full CLI query interface working against stored logs.

---

## Phase 6: Reliability

**Goal:** System tolerates crashes and reconnections gracefully.

- [ ] Client auto-reconnect on broker restart
- [ ] Client-side buffering during broker downtime
- [ ] Session cleanup on unexpected client disconnect
- [ ] Broker recovery — rebuild cache from DB on restart
- [ ] Broken pipe handling
- [ ] PID file / lock file for broker singleton
- [ ] Health check command (`devlog status`)

**Deliverable:** Kill the broker mid-stream, restart it, clients reconnect and resume.

---

## Phase 7: MCP Server

**Goal:** AI agents can query logs via MCP tools.

- [ ] MCP server implementation (stdio transport)
- [ ] `get_recent_logs` tool
- [ ] `search_logs` tool
- [ ] `get_errors` tool
- [ ] `get_sessions` tool
- [ ] `get_service_list` tool
- [ ] JSON response formatting optimized for AI consumption
- [ ] Tool parameter validation

**Deliverable:** Claude Code can query devlog via MCP tools in conversation.

---

## Phase 8: Polish & Distribution

**Goal:** Production-quality developer experience.

- [ ] Cross-platform support (Windows named pipes)
- [ ] Install script / Homebrew formula
- [ ] `devlog init` for project-level configuration
- [ ] Log level detection heuristics (ERROR, WARN from unstructured logs)
- [ ] Configurable data directory
- [ ] Database size management (retention / pruning)
- [ ] Man page / `--help` polish
- [ ] CI pipeline (build, test, release)

**Deliverable:** Installable, documented, ready for daily use.

---

## Future (Post-MVP)

Not scoped but worth tracking:

- JSON log parsing (structured field extraction)
- Trace ID / request ID extraction
- Stack trace grouping
- Web UI for log browsing
- HTTP API
- Remote log forwarding
- Plugin system
- Service config file (`.devlog.yaml`)
- Homebrew tap / APT package
