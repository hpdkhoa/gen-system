package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/mcp"
)

// a small project: Checkout calls save, save writes through database/sql.
const shopSrc = `package shop

import "database/sql"

// Checkout closes an order.
func Checkout(db *sql.DB, id int) error {
	if id <= 0 {
		return nil
	}
	return save(db, id)
}

func save(db *sql.DB, id int) error {
	_, err := db.Exec("UPDATE orders SET done = true WHERE id = $1", id)
	return err
}
`

func newTestApp(t *testing.T) *app {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shop.go"), []byte(shopSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return load(root, "", "none")
}

// callTool sends one tools/call through the JSON-RPC layer, as an agent would.
func callTool(t *testing.T, srv *mcp.Server, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	msg, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}})
	raw, _ := json.Marshal(srv.Handle(msg))
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if e, ok := out["error"]; ok {
		t.Fatalf("%s: protocol error %v", name, e)
	}
	res := out["result"].(map[string]any)
	isErr, _ := res["isError"].(bool)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var body map[string]any
	_ = json.Unmarshal([]byte(text), &body)
	if body == nil {
		body = map[string]any{"_text": text}
	}
	return body, isErr
}

func findSymbolID(t *testing.T, srv *mcp.Server, name string) string {
	t.Helper()
	body, isErr := callTool(t, srv, "search_symbols", map[string]any{"query": name})
	if isErr {
		t.Fatalf("search_symbols failed: %v", body)
	}
	var found string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case string:
			if found == "" && strings.Contains(x, "#") && strings.HasSuffix(x, ":"+name) {
				found = x
			}
		}
	}
	walk(body)
	if found == "" {
		t.Fatalf("no symbol id for %s in %v", name, body)
	}
	return found
}

// jsonEsc is s as it appears inside a JSON string (Windows paths double their backslashes).
func jsonEsc(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func TestToolsAnswerFromTheParser(t *testing.T) {
	a := newTestApp(t)
	srv := a.mcpServer(false)

	if body, isErr := callTool(t, srv, "architecture", nil); isErr || len(body) == 0 {
		t.Fatalf("architecture: %v", body)
	}

	checkout := findSymbolID(t, srv, "Checkout")
	save := findSymbolID(t, srv, "save")

	body, isErr := callTool(t, srv, "call_graph", map[string]any{"symbol": checkout, "depth": 1})
	raw, _ := json.Marshal(body)
	if isErr || !strings.Contains(string(raw), jsonEsc(save)) {
		t.Errorf("call_graph from Checkout must reach save: %s", raw)
	}

	body, isErr = callTool(t, srv, "source", map[string]any{"symbol": save})
	raw, _ = json.Marshal(body)
	if isErr || !strings.Contains(string(raw), "UPDATE orders") || strings.Contains(string(raw), "Checkout closes") {
		t.Errorf("source must hold save's lines and no others: %s", raw)
	}

	if body, isErr = callTool(t, srv, "control_flow", map[string]any{"symbol": checkout}); isErr {
		t.Errorf("control_flow: %v", body)
	}

	body, isErr = callTool(t, srv, "effects", map[string]any{"effect": "db"})
	raw, _ = json.Marshal(body)
	if isErr || !strings.Contains(string(raw), "save") {
		t.Errorf("effects db must list save, which calls db.Exec: %s", raw)
	}

	body, isErr = callTool(t, srv, "beliefs", map[string]any{"subject": "Checkout", "limit": 50})
	if isErr {
		t.Fatalf("beliefs: %v", body)
	}
	beliefs, _ := body["beliefs"].([]any)
	if len(beliefs) == 0 {
		t.Fatalf("beliefs about Checkout must not be empty: %v", body)
	}
	for _, b := range beliefs {
		if !strings.Contains(b.(map[string]any)["subject"].(string), "Checkout") {
			t.Errorf("the subject filter let through %v", b)
		}
	}

	if body, isErr = callTool(t, srv, "file_symbols", map[string]any{"file": "shop.go"}); isErr {
		t.Errorf("file_symbols: %v", body)
	}
}

func TestBadArgumentsAreToolErrors(t *testing.T) {
	srv := newTestApp(t).mcpServer(false)
	for name, args := range map[string]map[string]any{
		"search_symbols": {"query": ""},
		"call_graph":     {},
		"source":         {"symbol": 7},
		"beliefs":        {"limit": "ten"},
	} {
		if body, isErr := callTool(t, srv, name, args); !isErr {
			t.Errorf("%s(%v) must be a tool error, got %v", name, args, body)
		}
	}
}

func TestCorrectionsNeedTheFlag(t *testing.T) {
	a := newTestApp(t)
	list := func(allow bool) string {
		raw, _ := json.Marshal(a.mcpServer(allow).Handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
		return string(raw)
	}
	if strings.Contains(list(false), "correct_belief") {
		t.Error("correct_belief must not exist without --allow-corrections")
	}
	if !strings.Contains(list(true), "correct_belief") {
		t.Error("correct_belief must exist with --allow-corrections")
	}

	srv := a.mcpServer(true)
	body, _ := callTool(t, srv, "beliefs", map[string]any{"subject": "Checkout", "limit": 1})
	b := body["beliefs"].([]any)[0].(map[string]any)
	id := b["id"].(string)
	if body, isErr := callTool(t, srv, "correct_belief", map[string]any{"belief_id": id, "corrected_to": "closes an order", "note": "test"}); isErr {
		t.Fatalf("correct_belief: %v", body)
	}
	body, _ = callTool(t, srv, "beliefs", map[string]any{"subject": "Checkout", "limit": 500})
	found := false
	for _, x := range body["beliefs"].([]any) {
		m := x.(map[string]any)
		if m["id"] == id && m["human_verified"] == true {
			found = true
		}
	}
	if !found {
		t.Errorf("the corrected belief must read back as human verified")
	}
	if _, isErr := callTool(t, srv, "correct_belief", map[string]any{"belief_id": "no-such-belief", "corrected_to": "x"}); !isErr {
		t.Error("an unknown belief id must be a tool error")
	}
}
