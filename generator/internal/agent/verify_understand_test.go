// verify_understand_test.go — v25.5 (step 2): real-behaviour tests for the
// Understand-verify helpers that don't need a live model or a generated app.
package agent

import (
	"strings"
	"testing"
)

func TestParseVerifyMode(t *testing.T) {
	cases := map[string]VerifyMode{
		"":        VerifyOff,
		"off":     VerifyOff,
		"stop":    VerifyStop,
		"STOP":    VerifyStop,
		" stop ":  VerifyStop,
		"autofix": VerifyAutofix,
		"heal":    VerifyAutofix,
		"fix":     VerifyAutofix,
		"garbage": VerifyOff,
	}
	for in, want := range cases {
		if got := ParseVerifyMode(in); got != want {
			t.Errorf("ParseVerifyMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsReadOpName(t *testing.T) {
	read := []string{"GetUser", "ListOrders", "FindByEmail", "FetchAll", "SearchProducts", "QueryStock", "ViewCart"}
	write := []string{"Checkout", "ApplyDiscount", "AdjustStock", "Refund", "CreateOrder", "UpdateUser"}
	for _, n := range read {
		if !isReadOpName(n) {
			t.Errorf("isReadOpName(%q) = false, want true", n)
		}
	}
	for _, n := range write {
		if isReadOpName(n) {
			t.Errorf("isReadOpName(%q) = true, want false", n)
		}
	}
}

func TestResolutionPct_EmptyIs100(t *testing.T) {
	if p := (VerifyReport{}).ResolutionPct(); p != 100 {
		t.Errorf("empty report ResolutionPct = %v, want 100", p)
	}
	r := VerifyReport{ResolvedEdges: 3, TotalEdges: 4}
	if p := r.ResolutionPct(); p != 75 {
		t.Errorf("ResolutionPct = %v, want 75", p)
	}
}

func TestWriteFindingsReport(t *testing.T) {
	// No findings → clean-bill text.
	clean := WriteFindingsReport(VerifyReport{ResolvedEdges: 10, TotalEdges: 10})
	if !strings.Contains(clean, "No structural findings") {
		t.Error("clean report should state no findings")
	}
	if !strings.Contains(clean, "100.0%") {
		t.Errorf("clean report should show 100%% resolution; got:\n%s", clean)
	}

	// With findings → each finding rendered with kind + detail.
	rep := VerifyReport{
		ResolvedEdges: 8, TotalEdges: 10,
		Findings: []VerifyFinding{
			{Kind: "orphan", Symbol: "OrderService.Checkout", File: "backend/x.go",
				Detail: "service operation referenced but not defined"},
			{Kind: "effect", Symbol: "UserService.GetUser", File: "backend/y.go",
				Detail: "read-style operation has a DB-WRITE effect"},
		},
	}
	out := WriteFindingsReport(rep)
	for _, want := range []string{"ORPHAN", "EFFECT", "OrderService.Checkout", "UserService.GetUser", "80.0%", "backend/x.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("findings report missing %q; got:\n%s", want, out)
		}
	}
}

