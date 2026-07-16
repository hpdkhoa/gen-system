// generator/internal/ai/reviewer_v2325_test.go — v23.25 (task C): proves the
// backend-scoped severity tiers. SQL-injection / unvalidated-input findings are
// INFO on the in-memory backend (no SQL) and WARN on the postgres path; the
// scaffold ignored-error idiom is INFO; "no hardcoded secrets" style non-
// findings are dropped; Safe is recomputed from WARN-level survivors only.
package ai

import "testing"

func TestScopeIssues_InMemoryDowngradesSQL(t *testing.T) {
	raw := []string{
		"SQL injection: string concatenation inside query strings",
		"Unvalidated input: user data passed to DB without sanitization or type assertion",
		"Hardcoded secrets: the code does not contain any hardcoded secrets, which is good practice",
	}
	res := scopeIssues(raw, false /* in-memory */)
	if !res.Safe {
		t.Fatalf("in-memory backend with only SQL/unvalidated findings should be Safe, got unsafe; warnings=%v", res.Warnings())
	}
	if len(res.Warnings()) != 0 {
		t.Errorf("expected 0 warnings on in-memory backend, got %v", res.Warnings())
	}
	// The two real findings survive as INFO; the "does not contain" line is dropped.
	if got := len(res.InfoNotes()); got != 2 {
		t.Errorf("expected 2 info notes (SQL + unvalidated), got %d: %v", got, res.InfoNotes())
	}
}

func TestScopeIssues_PostgresKeepsSQLAsWarn(t *testing.T) {
	raw := []string{
		"SQL injection: string concatenation inside query strings",
	}
	res := scopeIssues(raw, true /* postgres */)
	if res.Safe {
		t.Fatalf("postgres backend with a SQL-injection finding must be unsafe")
	}
	if len(res.Warnings()) != 1 {
		t.Errorf("expected 1 warning on postgres path, got %v", res.Warnings())
	}
}

func TestScopeIssues_IgnoredErrorIsInfo(t *testing.T) {
	raw := []string{"Ignored errors: critical errors discarded with _ = someErr"}
	res := scopeIssues(raw, true) // even on postgres, the scaffold idiom is info
	if !res.Safe {
		t.Errorf("ignored-error idiom should not make the file unsafe")
	}
	if len(res.InfoNotes()) != 1 {
		t.Errorf("expected the ignored-error line as a single info note, got %v", res.InfoNotes())
	}
}

func TestScopeIssues_RealHardcodedSecretStaysWarn(t *testing.T) {
	raw := []string{`Hardcoded secrets: apiKey := "sk-live-12345" is a literal token`}
	res := scopeIssues(raw, false)
	if res.Safe {
		t.Fatalf("a genuine hardcoded secret must remain a warning even on in-memory backend")
	}
	if len(res.Warnings()) != 1 {
		t.Errorf("expected 1 warning for the hardcoded secret, got %v", res.Warnings())
	}
}
