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

// Notification is a JSON-RPC notification (no id).
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

func (t *Transport) WriteResponse(resp *Response) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(t.writer, string(b))
	return err
}

func (t *Transport) WriteNotification(n *Notification) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(t.writer, string(b))
	return err
}

func (t *Transport) SendResult(id any, result any) error {
	return t.WriteResponse(&Response{JSONRPC: "2.0", ID: id, Result: result})
}

func (t *Transport) SendError(id any, code int, msg string) error {
	return t.WriteResponse(&Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: msg}})
}

// Session update notification helpers

type SessionUpdate struct {
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Data      any    `json:"data,omitempty"`
}

func (t *Transport) SendSessionUpdate(sessionID, kind string, data any) error {
	return t.WriteNotification(&Notification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  SessionUpdate{SessionID: sessionID, Kind: kind, Data: data},
	})
}
