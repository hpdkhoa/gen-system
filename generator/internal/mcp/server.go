// Package mcp is a small Model Context Protocol server: JSON-RPC 2.0 over
// stdio, one JSON message per line, with tools only. It uses the standard
// library alone, so a binary built on it keeps gen-system's air-gapped,
// no-new-dependency promise.
//
// The server answers initialize, ping, tools/list and tools/call. It ignores
// notifications, as the protocol requires, and returns a JSON-RPC error for
// any other method.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// SupportedVersions lists the protocol versions this server speaks, newest
// first. initialize answers with the client's version when it is listed here,
// and with the newest one otherwise.
var SupportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Tool is one callable tool.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`

	// Handler runs the tool. A returned error becomes a tool result with
	// isError set, so the model sees the message and can correct its call.
	Handler func(args map[string]any) (any, error) `json:"-"`
}

// Server holds the tools and the identity it reports in initialize.
type Server struct {
	Name         string
	Version      string
	Instructions string

	// MaxText caps the text of one tool result in bytes. Zero means 200,000.
	MaxText int

	mu    sync.Mutex
	tools []Tool
	index map[string]int
}

// AddTool registers a tool. A later tool with the same name replaces it.
func (s *Server) AddTool(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index == nil {
		s.index = map[string]int{}
	}
	if i, ok := s.index[t.Name]; ok {
		s.tools[i] = t
		return
	}
	s.index[t.Name] = len(s.tools)
	s.tools = append(s.tools, t)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Serve reads requests from in and writes responses to out until in ends.
// It handles one request at a time, in order.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := sc.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		if resp := s.Handle(line); resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Handle answers one raw message. It returns nil for a notification.
func (s *Server) Handle(raw []byte) *response {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return &response{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{codeParse, "parse error: " + err.Error()}}
	}
	if len(req.ID) == 0 {
		return nil // a notification: no answer, whatever the method
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{codeInvalidRequest, "invalid request"}}
	}
	result, rerr := s.dispatch(req.Method, req.Params)
	if rerr != nil {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: rerr}
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		version := SupportedVersions[0]
		for _, v := range SupportedVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		res := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		s.mu.Lock()
		list := append([]Tool(nil), s.tools...)
		s.mu.Unlock()
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{codeInvalidParams, "tools/call needs a tool name"}
		}
		s.mu.Lock()
		i, ok := s.index[p.Name]
		var t Tool
		if ok {
			t = s.tools[i]
		}
		s.mu.Unlock()
		if !ok {
			return nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		return s.callTool(t, p.Arguments), nil
	default:
		return nil, &rpcError{codeMethodNotFound, "method not found: " + method}
	}
}

func (s *Server) callTool(t Tool, args map[string]any) map[string]any {
	out, err := safeCall(t.Handler, args)
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		}
	}
	text, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "result is not JSON: " + err.Error()}},
			"isError": true,
		}
	}
	max := s.MaxText
	if max <= 0 {
		max = 200000
	}
	res := map[string]any{}
	if len(text) > max {
		res["content"] = []map[string]any{{"type": "text", "text": string(text[:max]) +
			fmt.Sprintf("\n... cut at %d of %d bytes. Ask for less: a smaller limit, depth or a narrower filter.", max, len(text))}}
		return res
	}
	res["content"] = []map[string]any{{"type": "text", "text": string(text)}}
	// Clients from protocol 2025-06-18 on also read the object directly.
	var obj map[string]any
	if json.Unmarshal(text, &obj) == nil {
		res["structuredContent"] = obj
	}
	return res
}

// safeCall runs a handler and turns a panic into an error, so one bad call
// cannot stop the server.
func safeCall(h func(map[string]any) (any, error), args map[string]any) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: %v", r)
		}
	}()
	if h == nil {
		return nil, errors.New("tool has no handler")
	}
	return h(args)
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r' || b[j-1] == '\n') {
		j--
	}
	return b[i:j]
}

// Arg helpers for handlers. Each returns the zero value when the argument is
// missing, and an error when it is present with the wrong type.

// String reads a string argument.
func String(args map[string]any, name string) (string, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return s, nil
}

// Int reads an integer argument. JSON numbers arrive as float64.
func Int(args map[string]any, name string) (int, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	return int(f), nil
}

// Bool reads a boolean argument.
func Bool(args map[string]any, name string) (bool, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return b, nil
}
