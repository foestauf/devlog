package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/foestauf/devlog/internal/protocol"
)

// mockQuerier implements BrokerQuerier for testing without a real broker.
type mockQuerier struct {
	response *protocol.QueryResponse
	err      error
	lastQuery *protocol.QueryMessage
}

func (m *mockQuerier) Query(query *protocol.QueryMessage) (*protocol.QueryResponse, error) {
	m.lastQuery = query
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// sendRequest encodes a JSON-RPC request, runs the server, and returns the
// decoded response.
func sendRequest(t *testing.T, srv *Server, method string, id interface{}, params interface{}) jsonRPCResponse {
	t.Helper()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		req["params"] = json.RawMessage(raw)
	}

	line, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	line = append(line, '\n')

	reader := bytes.NewReader(line)
	var writer bytes.Buffer
	srv.reader = reader
	srv.writer = &writer

	if err := srv.Run(); err != nil {
		t.Fatalf("server run: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(writer.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", writer.String(), err)
	}
	return resp
}

func TestInitialize(t *testing.T) {
	mq := &mockQuerier{}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "initialize", 1, map[string]interface{}{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}

	info, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("missing serverInfo")
	}
	if info["name"] != "devlog" {
		t.Errorf("expected server name 'devlog', got %v", info["name"])
	}

	caps, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("missing capabilities")
	}
	if _, ok := caps["tools"]; !ok {
		t.Error("missing tools capability")
	}
}

func TestToolsListReturnsAllTools(t *testing.T) {
	mq := &mockQuerier{}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/list", 2, nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}

	toolsRaw, ok := result["tools"]
	if !ok {
		t.Fatal("missing tools key")
	}
	tools, ok := toolsRaw.([]interface{})
	if !ok {
		t.Fatalf("tools is not a slice: %T", toolsRaw)
	}

	if len(tools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(tools))
	}

	expectedNames := map[string]bool{
		"get_recent_logs":  false,
		"search_logs":      false,
		"get_errors":       false,
		"get_sessions":     false,
		"get_service_list": false,
	}
	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		if _, exists := expectedNames[name]; exists {
			expectedNames[name] = true
		}
	}
	for name, found := range expectedNames {
		if !found {
			t.Errorf("tool %q not found in tools list", name)
		}
	}
}

func TestToolCallGetRecentLogs(t *testing.T) {
	endedAt := int64(1700000060000)
	mq := &mockQuerier{
		response: &protocol.QueryResponse{
			Type: protocol.TypeResult,
			Events: []protocol.LogEvent{
				{
					ID:        1,
					SessionID: "42",
					Timestamp: 1700000000000,
					Stream:    "stdout",
					Message:   "server started on port 3000",
					Service:   "api",
				},
				{
					ID:        2,
					SessionID: "42",
					Timestamp: 1700000001000,
					Stream:    "stderr",
					Level:     "error",
					Message:   "connection refused",
					Service:   "api",
				},
			},
		},
	}
	_ = endedAt
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 3, map[string]interface{}{
		"name":      "get_recent_logs",
		"arguments": map[string]interface{}{"service": "api", "limit": 50},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// Verify the query was dispatched correctly.
	if mq.lastQuery.Command != "recent" {
		t.Errorf("expected command 'recent', got %q", mq.lastQuery.Command)
	}
	if mq.lastQuery.Service != "api" {
		t.Errorf("expected service 'api', got %q", mq.lastQuery.Service)
	}
	if mq.lastQuery.Limit != 50 {
		t.Errorf("expected limit 50, got %d", mq.lastQuery.Limit)
	}

	// Check the result contains formatted events.
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("missing content in result")
	}
	first := content[0].(map[string]interface{})
	text, _ := first["text"].(string)

	if !strings.Contains(text, "server started on port 3000") {
		t.Error("response text should contain the log message")
	}
	if !strings.Contains(text, "connection refused") {
		t.Error("response text should contain the error message")
	}
	if !strings.Contains(text, `"count":2`) {
		t.Error("response text should contain event count")
	}
}

func TestToolCallSearchLogs(t *testing.T) {
	mq := &mockQuerier{
		response: &protocol.QueryResponse{
			Type:   protocol.TypeResult,
			Events: []protocol.LogEvent{},
		},
	}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 4, map[string]interface{}{
		"name": "search_logs",
		"arguments": map[string]interface{}{
			"service": "web",
			"query":   "timeout",
			"since":   "2024-01-01T00:00:00Z",
		},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	if mq.lastQuery.Command != "search" {
		t.Errorf("expected command 'search', got %q", mq.lastQuery.Command)
	}
	if mq.lastQuery.Query != "timeout" {
		t.Errorf("expected query 'timeout', got %q", mq.lastQuery.Query)
	}
	if mq.lastQuery.Since == "" {
		t.Error("expected since to be set")
	}
}

func TestToolCallGetErrors(t *testing.T) {
	mq := &mockQuerier{
		response: &protocol.QueryResponse{
			Type:   protocol.TypeResult,
			Events: []protocol.LogEvent{},
		},
	}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 5, map[string]interface{}{
		"name":      "get_errors",
		"arguments": map[string]interface{}{"service": "worker"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if mq.lastQuery.Command != "errors" {
		t.Errorf("expected command 'errors', got %q", mq.lastQuery.Command)
	}
}

func TestToolCallGetSessions(t *testing.T) {
	endedAt := int64(1700000060000)
	mq := &mockQuerier{
		response: &protocol.QueryResponse{
			Type: protocol.TypeResult,
			Sessions: []protocol.SessionInfo{
				{
					ID:        "1",
					Service:   "api",
					PID:       12345,
					Command:   "go run ./cmd/api",
					StartedAt: 1700000000000,
					EndedAt:   &endedAt,
				},
			},
		},
	}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 6, map[string]interface{}{
		"name":      "get_sessions",
		"arguments": map[string]interface{}{"service": "api"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)

	if !strings.Contains(text, "go run ./cmd/api") {
		t.Error("response should contain the session command")
	}
	if !strings.Contains(text, `"count":1`) {
		t.Error("response should contain session count")
	}
}

func TestToolCallGetServiceList(t *testing.T) {
	mq := &mockQuerier{
		response: &protocol.QueryResponse{
			Type:     protocol.TypeResult,
			Services: []string{"api", "web", "worker"},
		},
	}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 7, map[string]interface{}{
		"name":      "get_service_list",
		"arguments": map[string]interface{}{},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)

	if !strings.Contains(text, "api") || !strings.Contains(text, "web") || !strings.Contains(text, "worker") {
		t.Error("response should contain all service names")
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	mq := &mockQuerier{}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 8, map[string]interface{}{
		"name":      "nonexistent_tool",
		"arguments": map[string]interface{}{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Errorf("expected 'unknown tool' in error, got %q", resp.Error.Message)
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	mq := &mockQuerier{}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "bogus/method", 9, nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

func TestInitializedNotificationNoResponse(t *testing.T) {
	mq := &mockQuerier{}

	// Send "notifications/initialized" — should produce no response.
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	line, _ := json.Marshal(req)
	line = append(line, '\n')

	reader := bytes.NewReader(line)
	var writer bytes.Buffer
	srv := newServerForTest(mq, reader, &writer)
	srv.reader = reader
	srv.writer = &writer

	if err := srv.Run(); err != nil {
		t.Fatalf("server run: %v", err)
	}

	if writer.Len() != 0 {
		t.Errorf("expected no output for notification, got %q", writer.String())
	}
}

func TestMissingRequiredServiceParam(t *testing.T) {
	mq := &mockQuerier{
		response: &protocol.QueryResponse{Type: protocol.TypeResult},
	}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "tools/call", 10, map[string]interface{}{
		"name":      "get_recent_logs",
		"arguments": map[string]interface{}{},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}

	// The result should indicate an error via isError.
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError=true when service is missing")
	}
}

func TestResponseFormatIsJSONRPC(t *testing.T) {
	mq := &mockQuerier{}
	srv := newServerForTest(mq, nil, nil)

	resp := sendRequest(t, srv, "initialize", 42, nil)
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %q", resp.JSONRPC)
	}
	// ID should round-trip as the number 42.
	idFloat, ok := resp.ID.(float64)
	if !ok || idFloat != 42 {
		t.Errorf("expected id 42, got %v (%T)", resp.ID, resp.ID)
	}
}
