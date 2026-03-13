package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Encoder writes newline-delimited JSON messages to a writer.
type Encoder struct {
	w io.Writer
}

// NewEncoder creates a new Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode serialises msg as JSON followed by a newline.
func (e *Encoder) Encode(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("protocol: marshal: %w", err)
	}
	data = append(data, '\n')
	_, err = e.w.Write(data)
	if err != nil {
		return fmt.Errorf("protocol: write: %w", err)
	}
	return nil
}

// Decoder reads newline-delimited JSON messages from a reader.
type Decoder struct {
	scanner *bufio.Scanner
}

// NewDecoder creates a new Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{scanner: bufio.NewScanner(r)}
}

// Decode reads the next newline-delimited JSON message into dest.
func (d *Decoder) Decode(dest interface{}) error {
	if !d.scanner.Scan() {
		if err := d.scanner.Err(); err != nil {
			return fmt.Errorf("protocol: scan: %w", err)
		}
		return io.EOF
	}
	data := d.scanner.Bytes()
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("protocol: unmarshal: %w", err)
	}
	return nil
}

// DecodeMessage takes raw JSON bytes and returns the appropriate typed message
// based on the "type" field in the envelope.
func DecodeMessage(data []byte) (interface{}, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("protocol: decode envelope: %w", err)
	}

	switch env.Type {
	case TypeSessionStart:
		var msg SessionStartMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode session_start: %w", err)
		}
		return &msg, nil

	case TypeLog:
		var msg LogMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode log: %w", err)
		}
		return &msg, nil

	case TypeSessionEnd:
		var msg SessionEndMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode session_end: %w", err)
		}
		return &msg, nil

	case TypeQuery:
		var msg QueryMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode query: %w", err)
		}
		return &msg, nil

	case TypeSessionStartReply:
		var msg SessionStartResponse
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode session_start_reply: %w", err)
		}
		return &msg, nil

	case TypeResult:
		var msg QueryResponse
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode result: %w", err)
		}
		return &msg, nil

	case TypeStatusReply:
		var msg StatusResponse
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("protocol: decode status_reply: %w", err)
		}
		return &msg, nil

	default:
		return nil, fmt.Errorf("protocol: unknown message type: %q", env.Type)
	}
}
