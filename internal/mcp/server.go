package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/foestauf/devlog/internal/ipc"
	"github.com/foestauf/devlog/internal/protocol"
)

const serverName = "devlog"

// JSON-RPC 2.0 types.

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP tool schema types.

type toolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string                `json:"type"`
	Properties map[string]property   `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     interface{} `json:"default,omitempty"`
}

// MCP content types for tool call responses.

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// BrokerQuerier abstracts the broker query mechanism so we can mock it in tests.
type BrokerQuerier interface {
	Query(query *protocol.QueryMessage) (*protocol.QueryResponse, error)
}

// socketQuerier connects to the broker's Unix socket to run queries.
type socketQuerier struct {
	socketPath string
}

func (sq *socketQuerier) Query(query *protocol.QueryMessage) (*protocol.QueryResponse, error) {
	conn, err := ipc.Dial(sq.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to broker: %w", err)
	}
	defer conn.Close()

	enc := protocol.NewEncoder(conn)
	if err := enc.Encode(query); err != nil {
		return nil, fmt.Errorf("send query: %w", err)
	}

	dec := protocol.NewDecoder(conn)
	var resp protocol.QueryResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}

// Server is the MCP server that reads JSON-RPC from stdin and writes to stdout.
type Server struct {
	dataDir string
	version string
	querier BrokerQuerier
	reader  io.Reader
	writer  io.Writer
}

// NewServer creates a Server that communicates via stdin/stdout and queries
// the broker through its Unix socket.
func NewServer(dataDir, version string) *Server {
	socketPath := filepath.Join(dataDir, "devlog.sock")
	return &Server{
		dataDir: dataDir,
		version: version,
		querier: &socketQuerier{socketPath: socketPath},
		reader:  os.Stdin,
		writer:  os.Stdout,
	}
}

// newServerForTest creates a Server with custom reader, writer, and querier
// for testing purposes.
func newServerForTest(querier BrokerQuerier, reader io.Reader, writer io.Writer) *Server {
	return &Server{
		dataDir: "/tmp/devlog-test",
		version: "test",
		querier: querier,
		reader:  reader,
		writer:  writer,
	}
}

// Run reads JSON-RPC requests from the reader and writes responses to the
// writer until EOF or an unrecoverable error.
func (s *Server) Run() error {
	scanner := bufio.NewScanner(s.reader)
	// Allow large messages (16MB).
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeResponse(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &rpcError{
					Code:    -32700,
					Message: "parse error: " + err.Error(),
				},
			})
			continue
		}

		resp := s.handleRequest(&req)
		if resp != nil {
			s.writeResponse(*resp)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

func (s *Server) writeResponse(resp jsonRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	data = append(data, '\n')
	s.writer.Write(data)
}

func (s *Server) handleRequest(req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized", "initialized":
		// Notification — no response required.
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

func (s *Server) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    serverName,
				"version": s.version,
			},
		},
	}
}

func (s *Server) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	tools := []toolDefinition{
		{
			Name:        "get_recent_logs",
			Description: "Get recent log lines for a service, ordered by most recent first.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"service": {Type: "string", Description: "Name of the service to query."},
					"limit":   {Type: "integer", Description: "Maximum number of log lines to return.", Default: 100},
				},
				Required: []string{"service"},
			},
		},
		{
			Name:        "search_logs",
			Description: "Search log messages for a service by substring match.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"service": {Type: "string", Description: "Name of the service to search."},
					"query":   {Type: "string", Description: "Substring to search for in log messages."},
					"since":   {Type: "string", Description: "Only return logs after this ISO 8601 timestamp."},
				},
				Required: []string{"service", "query"},
			},
		},
		{
			Name:        "get_errors",
			Description: "Get error log lines (stderr and error-level) for a service.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"service": {Type: "string", Description: "Name of the service to query."},
					"since":   {Type: "string", Description: "Only return errors after this ISO 8601 timestamp."},
				},
				Required: []string{"service"},
			},
		},
		{
			Name:        "get_sessions",
			Description: "List sessions, optionally filtered by service.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"service": {Type: "string", Description: "Filter sessions by service name."},
				},
			},
		},
		{
			Name:        "get_service_list",
			Description: "List all known service names.",
			InputSchema: inputSchema{
				Type:       "object",
				Properties: map[string]property{},
			},
		},
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func (s *Server) handleToolsCall(req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: "invalid params: " + err.Error(),
			},
		}
	}

	var result *toolCallResult
	switch params.Name {
	case "get_recent_logs":
		result = s.toolGetRecentLogs(params.Arguments)
	case "search_logs":
		result = s.toolSearchLogs(params.Arguments)
	case "get_errors":
		result = s.toolGetErrors(params.Arguments)
	case "get_sessions":
		result = s.toolGetSessions(params.Arguments)
	case "get_service_list":
		result = s.toolGetServiceList()
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: fmt.Sprintf("unknown tool: %s", params.Name),
			},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// Tool handlers — each builds a QueryMessage, sends it to the broker, and
// formats the response for AI consumption.

func (s *Server) toolGetRecentLogs(args json.RawMessage) *toolCallResult {
	var p struct {
		Service string `json:"service"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if p.Service == "" {
		return errorResult("service is required")
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}

	resp, err := s.querier.Query(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "recent",
		Service: p.Service,
		Limit:   p.Limit,
	})
	if err != nil {
		return errorResult("broker query failed: " + err.Error())
	}
	if resp.Error != "" {
		return errorResult(resp.Error)
	}

	return formatEventsResult(resp.Events)
}

func (s *Server) toolSearchLogs(args json.RawMessage) *toolCallResult {
	var p struct {
		Service string `json:"service"`
		Query   string `json:"query"`
		Since   string `json:"since"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if p.Service == "" {
		return errorResult("service is required")
	}
	if p.Query == "" {
		return errorResult("query is required")
	}

	query := &protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "search",
		Service: p.Service,
		Query:   p.Query,
	}
	if p.Since != "" {
		sinceMs, err := parseISO8601ToUnixMs(p.Since)
		if err != nil {
			return errorResult("invalid since timestamp: " + err.Error())
		}
		query.Since = fmt.Sprintf("%d", sinceMs)
	}

	resp, err := s.querier.Query(query)
	if err != nil {
		return errorResult("broker query failed: " + err.Error())
	}
	if resp.Error != "" {
		return errorResult(resp.Error)
	}

	return formatEventsResult(resp.Events)
}

func (s *Server) toolGetErrors(args json.RawMessage) *toolCallResult {
	var p struct {
		Service string `json:"service"`
		Since   string `json:"since"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("invalid arguments: " + err.Error())
	}
	if p.Service == "" {
		return errorResult("service is required")
	}

	query := &protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "errors",
		Service: p.Service,
	}
	if p.Since != "" {
		sinceMs, err := parseISO8601ToUnixMs(p.Since)
		if err != nil {
			return errorResult("invalid since timestamp: " + err.Error())
		}
		query.Since = fmt.Sprintf("%d", sinceMs)
	}

	resp, err := s.querier.Query(query)
	if err != nil {
		return errorResult("broker query failed: " + err.Error())
	}
	if resp.Error != "" {
		return errorResult(resp.Error)
	}

	return formatEventsResult(resp.Events)
}

func (s *Server) toolGetSessions(args json.RawMessage) *toolCallResult {
	var p struct {
		Service string `json:"service"`
	}
	if args != nil {
		json.Unmarshal(args, &p)
	}

	resp, err := s.querier.Query(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "sessions",
		Service: p.Service,
	})
	if err != nil {
		return errorResult("broker query failed: " + err.Error())
	}
	if resp.Error != "" {
		return errorResult(resp.Error)
	}

	return formatSessionsResult(resp.Sessions)
}

func (s *Server) toolGetServiceList() *toolCallResult {
	resp, err := s.querier.Query(&protocol.QueryMessage{
		Type:    protocol.TypeQuery,
		Command: "services",
	})
	if err != nil {
		return errorResult("broker query failed: " + err.Error())
	}
	if resp.Error != "" {
		return errorResult(resp.Error)
	}

	data, _ := json.Marshal(map[string]interface{}{
		"services": resp.Services,
	})
	return &toolCallResult{
		Content: []textContent{{Type: "text", Text: string(data)}},
	}
}

// Formatting helpers.

type logEventView struct {
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	Level     string `json:"level,omitempty"`
}

type sessionView struct {
	ID      string  `json:"id"`
	Service string  `json:"service"`
	Command string  `json:"command"`
	Started string  `json:"started_at"`
	Ended   *string `json:"ended_at,omitempty"`
}

func formatEventsResult(events []protocol.LogEvent) *toolCallResult {
	views := make([]logEventView, len(events))
	for i, e := range events {
		views[i] = logEventView{
			Timestamp: time.UnixMilli(e.Timestamp).UTC().Format(time.RFC3339),
			Service:   e.Service,
			Stream:    e.Stream,
			Message:   e.Message,
			Level:     e.Level,
		}
	}

	data, _ := json.Marshal(map[string]interface{}{
		"count":  len(views),
		"events": views,
	})
	return &toolCallResult{
		Content: []textContent{{Type: "text", Text: string(data)}},
	}
}

func formatSessionsResult(sessions []protocol.SessionInfo) *toolCallResult {
	views := make([]sessionView, len(sessions))
	for i, s := range sessions {
		views[i] = sessionView{
			ID:      s.ID,
			Service: s.Service,
			Command: s.Command,
			Started: time.UnixMilli(s.StartedAt).UTC().Format(time.RFC3339),
		}
		if s.EndedAt != nil {
			ended := time.UnixMilli(*s.EndedAt).UTC().Format(time.RFC3339)
			views[i].Ended = &ended
		}
	}

	data, _ := json.Marshal(map[string]interface{}{
		"count":    len(views),
		"sessions": views,
	})
	return &toolCallResult{
		Content: []textContent{{Type: "text", Text: string(data)}},
	}
}

func errorResult(msg string) *toolCallResult {
	return &toolCallResult{
		Content: []textContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func parseISO8601ToUnixMs(s string) (int64, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Try date-only format.
		t, err = time.Parse("2006-01-02", s)
		if err != nil {
			return 0, fmt.Errorf("expected RFC3339 or YYYY-MM-DD format")
		}
	}
	return t.UnixMilli(), nil
}
