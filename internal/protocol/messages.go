package protocol

import "encoding/json"

// Message type constants.
const (
	TypeSessionStart = "session_start"
	TypeLog          = "log"
	TypeSessionEnd   = "session_end"
	TypeQuery        = "query"
	TypeResult            = "result"
	TypeSessionStartReply = "session_start_reply"
	TypeStatusReply       = "status_reply"
)

// Envelope is used for initial decode to determine the message type.
type Envelope struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// SessionStartMessage is sent by a client to register a new session with the broker.
type SessionStartMessage struct {
	Type      string `json:"type"`
	Service   string `json:"service"`
	PID       int    `json:"pid"`
	Command   string `json:"command"`
	Timestamp int64  `json:"timestamp"`
}

// LogMessage is sent by a client to stream a log line to the broker.
type LogMessage struct {
	Type      string `json:"type"`
	Service   string `json:"service"`
	SessionID string `json:"session"`
	Timestamp int64  `json:"timestamp"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
}

// SessionEndMessage is sent by a client when its process exits.
type SessionEndMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session"`
	Timestamp int64  `json:"timestamp"`
	ExitCode  int    `json:"exit_code"`
}

// SessionStartResponse is sent by the broker back to the client after session registration.
type SessionStartResponse struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
}

// QueryMessage is sent by the CLI to request data from the broker.
type QueryMessage struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Service string `json:"service,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Query   string `json:"query,omitempty"`
	Since   string `json:"since,omitempty"`
}

// QueryResponse is returned by the broker in response to a QueryMessage.
type QueryResponse struct {
	Type     string        `json:"type"`
	Events   []LogEvent    `json:"events,omitempty"`
	Sessions []SessionInfo `json:"sessions,omitempty"`
	Services []string      `json:"services,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// LogEvent represents a single log line, used in query responses.
type LogEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Timestamp int64  `json:"timestamp"`
	Stream    string `json:"stream"`
	Level     string `json:"level,omitempty"`
	Message   string `json:"message"`
	Service   string `json:"service"`
}

// StatusResponse is returned by the broker in response to a "status" query.
type StatusResponse struct {
	Type           string         `json:"type"`
	PID            int            `json:"pid"`
	UptimeSeconds  float64        `json:"uptime_seconds"`
	ActiveSessions int            `json:"active_sessions"`
	QueueDepth     int            `json:"queue_depth"`
	Services       []string       `json:"services"`
	CacheEntries   map[string]int `json:"cache_entries"`
}

// SessionInfo represents metadata about a session, used in query responses.
type SessionInfo struct {
	ID        string `json:"id"`
	Service   string `json:"service"`
	PID       int    `json:"pid"`
	Command   string `json:"command"`
	StartedAt int64  `json:"started_at"`
	EndedAt   *int64 `json:"ended_at,omitempty"`
}
