package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func TestEncodeDecodeSessionStart(t *testing.T) {
	msg := SessionStartMessage{
		Type:      TypeSessionStart,
		Service:   "api",
		PID:       12345,
		Command:   "pnpm dev",
		Timestamp: 1710253200,
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(msg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got SessionStartMessage
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got != msg {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, msg)
	}
}

func TestEncodeDecodeLogMessage(t *testing.T) {
	msg := LogMessage{
		Type:      TypeLog,
		Service:   "api",
		SessionID: "session_12",
		Timestamp: 1710253200123,
		Stream:    "stdout",
		Message:   "Server started on :3000",
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(msg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got LogMessage
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got != msg {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, msg)
	}
}

func TestEncodeDecodeSessionEnd(t *testing.T) {
	msg := SessionEndMessage{
		Type:      TypeSessionEnd,
		SessionID: "session_12",
		Timestamp: 1710253260,
		ExitCode:  0,
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(msg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got SessionEndMessage
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got != msg {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, msg)
	}
}

func TestEncodeDecodeQuery(t *testing.T) {
	msg := QueryMessage{
		Type:    TypeQuery,
		Command: "recent",
		Service: "api",
		Limit:   200,
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(msg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got QueryMessage
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got != msg {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, msg)
	}
}

func TestEncodeDecodeQueryResponse(t *testing.T) {
	endedAt := int64(1710253260)
	msg := QueryResponse{
		Type: TypeResult,
		Events: []LogEvent{
			{
				ID:        1,
				SessionID: "session_12",
				Timestamp: 1710253200123,
				Stream:    "stdout",
				Level:     "info",
				Message:   "Server started on :3000",
				Service:   "api",
			},
		},
		Sessions: []SessionInfo{
			{
				ID:        "session_12",
				Service:   "api",
				PID:       12345,
				Command:   "pnpm dev",
				StartedAt: 1710253200,
				EndedAt:   &endedAt,
			},
		},
		Services: []string{"api", "worker"},
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(msg); err != nil {
		t.Fatalf("encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got QueryResponse
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Type != msg.Type {
		t.Errorf("type: got %q, want %q", got.Type, msg.Type)
	}
	if len(got.Events) != 1 {
		t.Fatalf("events: got %d, want 1", len(got.Events))
	}
	if got.Events[0].Message != "Server started on :3000" {
		t.Errorf("event message: got %q", got.Events[0].Message)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions: got %d, want 1", len(got.Sessions))
	}
	if got.Sessions[0].EndedAt == nil || *got.Sessions[0].EndedAt != endedAt {
		t.Errorf("session ended_at: got %v, want %d", got.Sessions[0].EndedAt, endedAt)
	}
	if len(got.Services) != 2 {
		t.Fatalf("services: got %d, want 2", len(got.Services))
	}
}

func TestDecodeMessageSessionStart(t *testing.T) {
	data := []byte(`{"type":"session_start","service":"api","pid":12345,"command":"pnpm dev","timestamp":1710253200}`)
	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	ss, ok := msg.(*SessionStartMessage)
	if !ok {
		t.Fatalf("expected *SessionStartMessage, got %T", msg)
	}
	if ss.Service != "api" || ss.PID != 12345 {
		t.Errorf("unexpected values: %+v", ss)
	}
}

func TestDecodeMessageLog(t *testing.T) {
	data := []byte(`{"type":"log","service":"api","session":"s1","timestamp":123,"stream":"stderr","message":"boom"}`)
	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	lm, ok := msg.(*LogMessage)
	if !ok {
		t.Fatalf("expected *LogMessage, got %T", msg)
	}
	if lm.Stream != "stderr" || lm.Message != "boom" {
		t.Errorf("unexpected values: %+v", lm)
	}
}

func TestDecodeMessageSessionEnd(t *testing.T) {
	data := []byte(`{"type":"session_end","session":"s1","timestamp":999,"exit_code":1}`)
	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	se, ok := msg.(*SessionEndMessage)
	if !ok {
		t.Fatalf("expected *SessionEndMessage, got %T", msg)
	}
	if se.ExitCode != 1 {
		t.Errorf("exit_code: got %d, want 1", se.ExitCode)
	}
}

func TestDecodeMessageQuery(t *testing.T) {
	data := []byte(`{"type":"query","command":"search","service":"worker","query":"timeout","limit":50}`)
	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	qm, ok := msg.(*QueryMessage)
	if !ok {
		t.Fatalf("expected *QueryMessage, got %T", msg)
	}
	if qm.Command != "search" || qm.Query != "timeout" {
		t.Errorf("unexpected values: %+v", qm)
	}
}

func TestDecodeMessageResult(t *testing.T) {
	data := []byte(`{"type":"result","error":"something went wrong"}`)
	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	qr, ok := msg.(*QueryResponse)
	if !ok {
		t.Fatalf("expected *QueryResponse, got %T", msg)
	}
	if qr.Error != "something went wrong" {
		t.Errorf("error: got %q", qr.Error)
	}
}

func TestDecodeMessageUnknownType(t *testing.T) {
	data := []byte(`{"type":"bogus"}`)
	_, err := DecodeMessage(data)
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestDecodeMessageInvalidJSON(t *testing.T) {
	_, err := DecodeMessage([]byte(`not json at all`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestMultipleMessagesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	messages := []interface{}{
		SessionStartMessage{Type: TypeSessionStart, Service: "api", PID: 1, Command: "go run .", Timestamp: 100},
		LogMessage{Type: TypeLog, Service: "api", SessionID: "s1", Timestamp: 101, Stream: "stdout", Message: "hello"},
		LogMessage{Type: TypeLog, Service: "api", SessionID: "s1", Timestamp: 102, Stream: "stderr", Message: "warn"},
		SessionEndMessage{Type: TypeSessionEnd, SessionID: "s1", Timestamp: 200, ExitCode: 0},
	}

	for _, msg := range messages {
		if err := enc.Encode(msg); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}

	dec := NewDecoder(&buf)

	var ss SessionStartMessage
	if err := dec.Decode(&ss); err != nil {
		t.Fatalf("decode session_start: %v", err)
	}
	if ss.Service != "api" {
		t.Errorf("session_start service: got %q", ss.Service)
	}

	var lm1 LogMessage
	if err := dec.Decode(&lm1); err != nil {
		t.Fatalf("decode log 1: %v", err)
	}
	if lm1.Message != "hello" {
		t.Errorf("log 1 message: got %q", lm1.Message)
	}

	var lm2 LogMessage
	if err := dec.Decode(&lm2); err != nil {
		t.Fatalf("decode log 2: %v", err)
	}
	if lm2.Stream != "stderr" {
		t.Errorf("log 2 stream: got %q", lm2.Stream)
	}

	var se SessionEndMessage
	if err := dec.Decode(&se); err != nil {
		t.Fatalf("decode session_end: %v", err)
	}
	if se.ExitCode != 0 {
		t.Errorf("session_end exit_code: got %d", se.ExitCode)
	}

	// Should get EOF after all messages consumed.
	var dummy json.RawMessage
	if err := dec.Decode(&dummy); err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestEncoderNewlineDelimited(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	if err := enc.Encode(SessionStartMessage{Type: TypeSessionStart, Service: "x"}); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if data[len(data)-1] != '\n' {
		t.Error("encoded message does not end with newline")
	}

	// Verify it's valid JSON without the trailing newline.
	trimmed := bytes.TrimSpace(data)
	if !json.Valid(trimmed) {
		t.Errorf("encoded data is not valid JSON: %s", trimmed)
	}
}

func TestSessionInfoNullEndedAt(t *testing.T) {
	info := SessionInfo{
		ID:        "s1",
		Service:   "api",
		PID:       99,
		Command:   "node index.js",
		StartedAt: 1000,
		EndedAt:   nil,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SessionInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.EndedAt != nil {
		t.Errorf("expected nil EndedAt, got %v", got.EndedAt)
	}
}
