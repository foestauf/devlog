# DevLog

A local development log broker that captures, persists, and queries logs from running dev services. Built for humans and AI agents alike.

DevLog sits between your dev processes and your terminal — capturing stdout/stderr, persisting to SQLite, and exposing structured queries via CLI and MCP tools. Your terminal output stays identical; your logs become durable and searchable.

---

## Install

### Homebrew (macOS and Linux)

```bash
brew install --cask foestauf/tap/devlog
```

Or, if you prefer tapping first:

```bash
brew tap foestauf/tap
brew install --cask devlog
```

Prebuilt binaries for `darwin_amd64`, `darwin_arm64`, `linux_amd64`, and `linux_arm64` are published to the [foestauf/homebrew-tap](https://github.com/foestauf/homebrew-tap) tap on each release.

### Build from source

Requires Go 1.21+ and CGO (for SQLite).

```bash
git clone https://github.com/foestauf/devlog.git
cd devlog
make build
```

The binary lands at `bin/devlog`. Copy it somewhere on your `$PATH`:

```bash
cp bin/devlog ~/.local/bin/
# or
sudo cp bin/devlog /usr/local/bin/
```

---

## Quick Start

```bash
# Instead of:
pnpm dev

# Run through devlog:
devlog run --service api -- pnpm dev
```

That's it. The broker auto-starts in the background on first use, logs flow to SQLite, and your terminal looks exactly the same.

### Wrapping multiple services

Run each in its own terminal (or use tmux/screen):

```bash
devlog run --service api -- pnpm dev
devlog run --service worker -- node worker.js
devlog run --service web -- npm run serve
```

Each service gets its own log stream, session tracking, and in-memory cache. All share the same broker and database.

---

## CLI Reference

### `devlog run` — Wrap a process

```bash
devlog run --service <name> -- <command> [args...]
```

Spawns the command, captures stdout/stderr, and streams log events to the broker. Output is mirrored to your terminal transparently — it looks and feels identical to running the command directly. Exit codes are forwarded.

The `--service` flag is required and tags all logs from this process.

```bash
devlog run --service api -- pnpm dev
devlog run --service worker -- go run ./cmd/worker
devlog run --service postgres -- docker compose up db
```

### `devlog recent` — Recent logs

```bash
devlog recent <service> [--limit N]
```

Shows the most recent log lines for a service. Served from the in-memory cache, so it's fast (< 10ms). Default limit is 100.

```bash
devlog recent api              # last 100 lines
devlog recent api -n 500       # last 500 lines
```

### `devlog search` — Search logs

```bash
devlog search <service> <query> [--since <duration>]
```

Searches log messages by substring. Hits SQLite, so it works across the full history, not just what's in the cache.

```bash
devlog search api "timeout"
devlog search worker "connection refused" --since 30m
```

### `devlog errors` — Error logs

```bash
devlog errors <service> [--since <duration>]
```

Returns stderr lines and any lines with detected error-level keywords. Default `--since` is `1h`.

```bash
devlog errors api                # errors in the last hour
devlog errors api --since 10m    # errors in the last 10 minutes
devlog errors worker --since 24h
```

### `devlog sessions` — Session history

```bash
devlog sessions [service]
```

Lists sessions (process runs) with start/end times, PIDs, and commands. Omit the service to see all sessions.

```bash
devlog sessions api
devlog sessions
```

### `devlog services` — List services

```bash
devlog services
```

Lists all service names that have ever sent logs.

### `devlog status` — Broker health

```bash
devlog status
```

Shows whether the broker is running and lists active services.

---

## Data Storage

DevLog stores everything under `~/.devlog/`:

| File | Purpose |
|------|---------|
| `~/.devlog/devlog.db` | SQLite database (WAL mode) |
| `~/.devlog/devlog.sock` | Unix domain socket for IPC |
| `~/.devlog/devlog.pid` | Broker PID file |

The broker starts automatically when you first run `devlog run`. It stays running in the background to accept connections from multiple services. It shuts down cleanly on SIGINT/SIGTERM.

---

## MCP Integration

DevLog includes an MCP server so AI agents (Claude Code, Cursor, etc.) can query your dev logs directly.

### Setup with Claude Code

Add to your Claude Code MCP settings (`~/.claude/claude_desktop_config.json` or project-level `.mcp.json`):

```json
{
  "mcpServers": {
    "devlog": {
      "command": "devlog",
      "args": ["mcp-server"]
    }
  }
}
```

### Available Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `get_recent_logs` | Recent log lines for a service | `service` (required), `limit` (default 100) |
| `search_logs` | Search logs by substring | `service`, `query` (required), `since` (ISO 8601, optional) |
| `get_errors` | Error-level logs for a service | `service` (required), `since` (ISO 8601, optional) |
| `get_sessions` | List sessions | `service` (optional) |
| `get_service_list` | All known service names | none |

Responses are formatted for AI consumption — clean JSON with timestamps, service names, and message text.

---

## Architecture

```
devlog run --service api -- pnpm dev
    │
    ▼
┌─────────┐     Unix Socket     ┌─────────┐
│  Client  │ ──────────────────▶ │  Broker  │
│ (wrapper)│                     │          │
└─────────┘                     │  ┌─────┐ │
                                │  │Cache │ │  ← in-memory ring buffers
                                │  └──┬──┘ │
                                │     │    │
                                │  ┌──▼──┐ │
                                │  │SQLite│ │  ← WAL mode, batch writes
                                │  └─────┘ │
                                │          │
                                │  ┌─────┐ │
                                │  │ MCP  │ │  ← AI agent interface
                                │  └─────┘ │
                                └─────────┘
```

- **Client** — wraps dev processes, captures stdout/stderr, streams to broker over Unix socket
- **Broker** — central coordinator, accepts connections, batches writes, serves queries
- **Storage** — SQLite with WAL mode, single-writer model, batch inserts
- **Cache** — per-service ring buffers (10k lines each) for fast recent queries
- **MCP Server** — stdio JSON-RPC server exposing 5 tools for AI agents

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full technical breakdown.

---

## Development

```bash
make build      # Build to bin/devlog
make test       # Run all tests
make vet        # Run go vet
make all        # Vet + test + build
```

See [ROADMAP.md](ROADMAP.md) for the phased implementation plan.

---

## License

TBD
