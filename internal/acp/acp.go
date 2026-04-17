package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC request/response types

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Transport handles stdin/stdout line-delimited JSON-RPC.
type Transport struct {
	scanner *bufio.Scanner
	writer  io.Writer
	mu      sync.Mutex
}

func NewTransport(r io.Reader, w io.Writer) *Transport {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	return &Transport{scanner: s, writer: w}
}

func (t *Transport) ReadRequest() (*Request, error) {
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	var req Request
	if err := json.Unmarshal(t.scanner.Bytes(), &req); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &req, nil
}

func (t *Transport) write(v any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(t.writer, string(b))
	return err
}

func (t *Transport) SendResult(id any, result any) error {
	return t.write(&Response{JSONRPC: "2.0", ID: id, Result: result})
}

func (t *Transport) SendError(id any, code int, msg string) error {
	return t.write(&Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: msg}})
}

// ACP standard session/update notifications
// Format: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"...","update":{...}}}

type sessionUpdateParams struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

// SendTextChunk sends an agent_message_chunk notification.
func (t *Transport) SendTextChunk(sessionID, text string) error {
	return t.write(&Notification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionID: sessionID,
			Update: map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]string{"text": text},
			},
		},
	})
}

// SendModelResolved notifies the client that the active model has been resolved or changed
// (e.g. auto-pick on session start, or fallback after a model failure).
// Non-standard ACP extension — clients unaware of it will simply ignore the update.
func (t *Transport) SendModelResolved(sessionID, model, reason string) error {
	return t.write(&Notification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionID: sessionID,
			Update: map[string]any{
				"sessionUpdate": "model_resolved",
				"model":         model,
				"reason":        reason,
			},
		},
	})
}

// SendToolCall sends a tool_call notification (status: running).
func (t *Transport) SendToolCall(sessionID, toolCallID, title string) error {
	return t.write(&Notification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionID: sessionID,
			Update: map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    toolCallID,
				"title":         title,
				"status":        "running",
			},
		},
	})
}

// SendToolCallUpdate sends a tool_call_update notification (completed/error).
func (t *Transport) SendToolCallUpdate(sessionID, toolCallID, title, status, output string) error {
	update := map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    toolCallID,
		"title":         title,
		"status":        status,
		"content":       map[string]string{"text": output},
	}
	if status == "error" {
		update["error"] = output
	}
	return t.write(&Notification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  sessionUpdateParams{SessionID: sessionID, Update: update},
	})
}
