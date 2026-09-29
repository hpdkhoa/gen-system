// generator/internal/ai/reviewer_scope_v2658_test.go — v26.58.
//
// D047: scopeIssues dropped any line that held a praise phrase as a substring,
// so "... is not good practice" or "does not contain any check" hid a real
// finding and the file was reported as clean.
package ai

import "testing"

func TestScopeIssues_NegatedPraiseIsAFinding(t *testing.T) {
	raw := []string{"Hardcoded API key in NewClient; storing secrets in source is not good practice."}
	res := scopeIssues(raw, false)
	if res.Safe || len(res.Warnings()) != 1 {
		t.Fatalf("hardcoded key finding was dropped: Safe=%v warnings=%v", res.Safe, res.Warnings())
	}
}

func TestScopeIssues_MissingCheckIsAFinding(t *testing.T) {
	raw := []string{"Delete handler does not contain any check that the caller owns the record."}
	res := scopeIssues(raw, false)
	if res.Safe || len(res.Warnings()) != 1 {
		t.Fatalf("missing check finding was dropped: Safe=%v warnings=%v", res.Safe, res.Warnings())
	}
}

func TestScopeIssues_NonFindingsStillDropped(t *testing.T) {
	raw := []string{
		"The code does not contain any hardcoded secrets.",
		"No hardcoded credentials were found.",
		"Errors from Save are returned to the caller, which is good.",
		"Input is checked before use, which is good practice.",
	}
	res := scopeIssues(raw, true)
	if !res.Safe || len(res.Findings) != 0 {
		t.Fatalf("non findings should be dropped: Safe=%v findings=%v", res.Safe, res.Findings)
	}
}
