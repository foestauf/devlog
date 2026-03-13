# DevLog — Claude Code Instructions

## Project Overview

DevLog is a local development log broker written in Go. It captures stdout/stderr from dev processes, persists to SQLite, and exposes structured queries via CLI and MCP tools.

## Architecture

Four components: Client (process wrapper), Broker (coordinator), Storage (SQLite), MCP Server (AI interface). See `docs/ARCHITECTURE.md` for details and `PLAN.md` for full requirements.

## Tech Stack

- **Language:** Go
- **Storage:** SQLite (WAL mode, single-writer)
- **IPC:** Unix domain sockets (Mac/Linux), named pipes (Windows)
- **Transport:** JSON messages over socket
- **Build:** Single static binary

## Project Structure

```
cmd/devlog/          — CLI entrypoint
internal/
  broker/            — Broker process, connection handling, query routing
  client/            — Process wrapper, log capture, socket streaming
  storage/           — SQLite operations, migrations, batch writer
  cache/             — In-memory ring buffers
  protocol/          — JSON message types, serialization
  mcp/               — MCP server, tool definitions
  session/           — Session lifecycle management
```

## Development Commands

```bash
go build -o devlog ./cmd/devlog    # Build
go test ./...                       # Test
go vet ./...                        # Lint
```

## Key Design Decisions

- **Single writer:** Only the broker writes to SQLite. Clients never touch the DB.
- **Non-blocking clients:** Broker queues events internally. Client writes never block on DB.
- **Cache is not truth:** Memory ring buffers are rebuilt from DB on restart. SQLite is the source of truth.
- **Auto-start broker:** Client detects missing broker and starts it automatically. No separate daemon command.
- **Transparent terminal:** Process wrapper must mirror stdout/stderr exactly. Developer UX must feel identical to running the process directly.

## Conventions

- Use standard Go project layout
- Error handling: return errors, don't panic
- Tests alongside source files (`_test.go`)
- Keep packages focused — one responsibility per package
- Use `context.Context` for cancellation and timeouts
- Prefer channels and goroutines for concurrency over mutexes where practical

## Performance Targets

- Broker detection: < 50ms
- Insert latency: < 5ms per batch
- Recent query: < 10ms
- Memory: < 100MB typical
- Throughput: 50k+ lines/min
