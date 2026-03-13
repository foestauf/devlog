# DevLog – Local Development Log Broker Requirements

## Purpose

DevLog is a local developer tool designed to capture logs from running development services and make them queryable by both humans and AI agents (Claude Code, Codex, etc.) without requiring developers to manually manage additional infrastructure.

The system must:

- Capture stdout/stderr from local services
- Persist logs safely
- Provide fast recent-log access
- Allow structured querying
- Support AI tool integration via MCP
- Require minimal developer workflow changes

---

# Core Design Goals

## Primary Goals

- Zero extra startup steps for developers
- Single binary deployment
- Resilient to crashes
- Fast recent log access
- Structured querying for AI agents
- Cross-platform operation
- Minimal resource usage

## Non-Goals (v1)

- Distributed logging
- Production observability replacement
- Metrics collection
- Complex indexing engines
- Full text search engines (Elastic, etc.)

---

# Architecture Overview

System consists of four major components:

## Client (Process Wrapper)

Captures logs from running services.

Responsibilities:

- Spawn development processes
- Capture stdout/stderr
- Tag logs with metadata
- Send events to broker
- Mirror logs to terminal
- Reconnect automatically if broker restarts

Command example:

```
devlog run --service api -- pnpm dev
```

---

## Broker (Coordinator Process)

Central authority managing:

- SQLite writes
- Memory caches
- Session tracking
- Client connections
- Query handling
- MCP integration

Responsibilities:

- Single writer to database
- Accept log streams from clients
- Maintain in-memory recent logs
- Batch database writes
- Serve queries

Broker must auto-start if missing.

---

## Storage Layer

Uses SQLite as durable storage.

Requirements:

- WAL mode enabled
- Single writer model
- Concurrent reads allowed
- Crash safe
- Fast inserts

Database location:

```
~/.devlog/devlog.db
```

---

## MCP Server

Exposes structured tools for AI agents.

Responsibilities:

- Tool definitions
- Query handlers
- Broker communication
- JSON responses

MCP must expose tools such as:

### Required MCP Tools

get_recent_logs
Parameters:

- service
- limit

search_logs
Parameters:

- service
- query
- since optional

get_errors
Parameters:

- service
- since

get_sessions
Parameters:

- service optional

get_service_list

---

# Process Model

## Startup Behavior

Client must:

1 Check broker socket
2 Start broker if missing
3 Connect to broker
4 Register session
5 Begin streaming logs

Broker must:

- Start automatically
- Create socket
- Initialize database
- Accept clients

---

# IPC Requirements

Transport:

- Unix domain socket (Mac/Linux)
- Named pipes (Windows)

Protocol:

- JSON messages
- Append-only events
- Lightweight messages

Example event:

```json
{
  "type": "log",
  "service": "api",
  "session": "session_12",
  "timestamp": 1710253200,
  "stream": "stdout",
  "message": "Server started"
}
```

---

# Storage Requirements

## Tables

### sessions

Fields:

- id
- service
- pid
- command
- started_at
- ended_at
- hostname optional

### log_events

Fields:

- id
- session_id
- timestamp
- stream
- level nullable
- message
- service

Optional future:

- trace_id
- request_id
- event_group_id

---

# Memory Cache Requirements

Broker must maintain:

Per-service ring buffers.

Requirements:

- Store recent logs (default 10k lines)
- Fast retrieval
- Rebuilt from DB on restart
- Not source of truth

Memory is cache only.
SQLite is truth.

---

# Client Requirements

Client must:

- Detect broker automatically
- Start broker if needed
- Stream logs line by line
- Support reconnect
- Preserve terminal UX

Terminal behavior must feel identical to native process execution.

Must mirror:

stdout
stderr

---

# Broker Requirements

Broker must:

- Accept multiple clients
- Track sessions
- Queue events
- Batch writes
- Maintain caches
- Support queries

Must avoid blocking clients on database writes.

Design:

```
client events
→ broker queue
→ memory cache
→ batch writer
→ sqlite
```

---

# Query Requirements

CLI must support:

recent:

```
devlog recent api --limit 200
```

search:

```
devlog search api "timeout"
```

errors:

```
devlog errors api --since 10m
```

sessions:

```
devlog sessions api
```

services:

```
devlog services
```

---

# Reliability Requirements

System must tolerate:

- Broker crashes
- Client crashes
- Service restarts
- Broken pipes

Behavior:

Broker restart:
Clients reconnect automatically.

Client exit:
Session closed.

Service restart:
New session created.

Database corruption:
Broker must fail safely.

---

# Session Model

Each service execution must create a session.

Session lifecycle:

Start:
service launched

Active:
logs flowing

End:
process exits

Session markers must exist so logs remain understandable.

Example:

```
API session 14 started
API session 14 ended
API session 15 started
```

---

# Performance Requirements

Target performance:

Startup:
< 50ms broker detection

Insert latency:
< 5ms per batch

Recent query:
< 10ms

Memory:
< 100MB typical usage

Throughput:
Handle 50k+ lines/minute

---

# Developer Experience Requirements

Must require no extra steps.

Developers should only:

Run normal dev commands.

Example:

Instead of:

```
pnpm dev
```

They run:

```
pnpm dev
```

Internally:

```
devlog run --service api -- pnpm dev
```

No separate daemon command required.

---

# AI Agent Optimization Requirements

System must support:

Structured queries instead of raw log dumps.

Must allow:

Recent context retrieval
Error filtering
Time filtering
Service separation
Session awareness

Avoid:

Returning massive unstructured blobs.

Preferred output:

Small relevant context windows.

---

# Future Expansion (Not Required v1)

Possible additions:

JSON log parsing
Trace ID extraction
Stack trace grouping
Log level detection
Web UI
HTTP API
Remote forwarding
Plugin system
Service config file

---

# Initial Feature Scope (MVP)

Must include:

Process wrapper
Broker
SQLite storage
Memory cache
Unix socket IPC
Session tracking
Recent logs query
Search query
Error filter
MCP tools
Auto broker start

---

# Technology Stack

Language:
Go

Storage:
SQLite

IPC:
Unix socket / named pipe

Transport:
JSON

Concurrency:
Goroutines

Build target:
Single static binary

---

# Success Criteria

Tool is successful if:

Developers run it without thinking about it.

AI agents can query logs without copy paste.

Logs survive crashes.

System never blocks development workflow.

Binary runs without setup.

---

# Guiding Principle

**Make local development logs structured, durable, and queryable without changing how developers work.**
