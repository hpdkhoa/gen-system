package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func newTestServer() *Server {
	s := &Server{Name: "test", Version: "1.0", Instructions: "hello"}
	s.AddTool(Tool{
		Name:        "echo",
		Description: "returns its input",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(args map[string]any) (any, error) {
			v, err := String(args, "text")
			if err != nil {
				return nil, err
			}
			return map[string]any{"text": v}, nil
		},
	})
	s.AddTool(Tool{
		Name: "fail", Description: "always fails", InputSchema: map[string]any{"type": "object"},
		Handler: func(map[string]any) (any, error) { return nil, errors.New("it failed") },
	})
	s.AddTool(Tool{
		Name: "boom", Description: "panics", InputSchema: map[string]any{"type": "object"},
		Handler: func(map[string]any) (any, error) { panic("boom") },
	})
	return s
}

func call(t *testing.T, s *Server, msg string) map[string]any {
	t.Helper()
	resp := s.Handle([]byte(msg))
	if resp == nil {
		t.Fatalf("no response to %s", msg)
	}
	raw, _ := json.Marshal(resp)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	s := newTestServer()
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Errorf("a supported client version must be echoed, got %v", res["protocolVersion"])
	}
	if res["instructions"] != "hello" {
		t.Errorf("instructions missing: %v", res)
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("tools capability missing: %v", res)
	}
	out = call(t, s, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := out["result"].(map[string]any)["protocolVersion"]; v != SupportedVersions[0] {
		t.Errorf("an unknown client version must get the newest, got %v", v)
	}
}

func TestNotificationGetsNoAnswer(t *testing.T) {
	s := newTestServer()
	if resp := s.Handle([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); resp != nil {
		t.Fatalf("a notification must not be answered, got %+v", resp)
	}
}

func TestToolsListAndCall(t *testing.T) {
	s := newTestServer()
	out := call(t, s, `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 || tools[0].(map[string]any)["name"] != "echo" {
		t.Fatalf("tools/list = %v", tools)
	}
	if _, ok := tools[0].(map[string]any)["inputSchema"]; !ok {
		t.Errorf("inputSchema missing")
	}
	out = call(t, s, `{"jsonrpc":"2.0","id":"b","method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
	res := out["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("echo failed: %v", res)
	}
	if sc := res["structuredContent"].(map[string]any); sc["text"] != "hi" {
		t.Errorf("structuredContent = %v", sc)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"hi"`) {
		t.Errorf("text content = %q", text)
	}
}

func TestToolErrorsAreResults(t *testing.T) {
	s := newTestServer()
	for _, name := range []string{"fail", "boom"} {
		out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`"}}`)
		res, ok := out["result"].(map[string]any)
		if !ok || res["isError"] != true {
			t.Errorf("%s: a failing tool must return a result with isError, got %v", name, out)
		}
	}
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":5}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("a wrong argument type must be a tool error, got %v", out)
	}
}

func TestProtocolErrors(t *testing.T) {
	s := newTestServer()
	cases := map[string]float64{
		`{not json`: codeParse,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`:                             codeMethodNotFound,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`: codeInvalidParams,
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`:                             codeInvalidRequest,
	}
	for msg, code := range cases {
		out := call(t, s, msg)
		e, ok := out["error"].(map[string]any)
		if !ok || e["code"] != code {
			t.Errorf("%s: want error %v, got %v", msg, code, out)
		}
	}
}

func TestLongResultIsCut(t *testing.T) {
	s := newTestServer()
	s.MaxText = 50
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"`+strings.Repeat("x", 200)+`"}}}`)
	res := out["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "cut at 50") {
		t.Errorf("a long result must say it was cut: %q", text)
	}
	if _, ok := res["structuredContent"]; ok {
		t.Errorf("a cut result must not carry structuredContent")
	}
}

func TestServeOverStreams(t *testing.T) {
	s := newTestServer()
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if err := s.Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 answers (the notification gets none), got %d: %s", len(lines), out.String())
	}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil || m["jsonrpc"] != "2.0" {
			t.Errorf("bad answer line %q", l)
		}
	}
}
