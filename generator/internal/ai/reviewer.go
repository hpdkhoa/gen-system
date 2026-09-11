// generator/internal/ai/reviewer.go — v23.25 (task C): backend-scoped severity
// tiers. The reviewer used to fire "SQL injection" + "unvalidated input" on
// EVERY in-memory CRUD file (no SQL anywhere), producing ~15 false-positive
// lines per continue run and eroding trust in [review] output. Now:
//   - Findings carry a Severity (warn / info).
//   - SQL-injection + unvalidated-input findings are only WARNINGS on the
//     postgres store path (GEN_BACKEND=postgres). On the in-memory backend they
//     are downgraded to INFO (there is no SQL string to inject into), and the
//     prompt tells the model so up front.
//   - The generated `_ =` ignored-error idiom (intentional, in scaffold code) is
//     downgraded to INFO.
//   - Safe stays true unless at least one WARN-level issue remains, so the
//     [review] block only shouts when something actually warrants attention.
//
// ai must NOT import engine (engine imports ai); backend mode is read straight
// from GEN_BACKEND here to keep the dependency direction intact.
package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Severity classifies a review finding.
type Severity string

const (
	SevWarn Severity = "warn" // worth surfacing + feeding to repair
	SevInfo Severity = "info" // noted, not alarming (e.g. no-SQL backend)
)

// Issue is a single review finding with a severity.
type Issue struct {
	Text     string   `json:"text"`
	Severity Severity `json:"severity"`
}

// ReviewResult is the structured output of a security review pass.
// Safe is true when no WARN-level issue remains after scoping.
// Issues retains the raw issue strings for backwards compatibility; Findings
// carries the severity-tagged view.
type ReviewResult struct {
	Safe     bool     `json:"safe"`
	Issues   []string `json:"issues"`
	Findings []Issue  `json:"-"`
}

// postgresBackend reports whether generation is targeting the Postgres store
// path, where SQL-injection / unvalidated-input findings are genuine warnings.
func postgresBackend() bool {
	return strings.EqualFold(os.Getenv("GEN_BACKEND"), "postgres")
}

// ReviewCode runs a second LLM pass on a generated file to detect security
// issues: SQL injection, missing validation, hardcoded secrets, ignored errors.
//
// Called by executor.go after every generate_* task.
// On AI failure: returns Safe:true so a broken AI never blocks builds.
func ReviewCode(taskType, entityName, filePath string) ReviewResult {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ReviewResult{Safe: true} // file doesn't exist yet — skip
	}
	if len(data) == 0 {
		return ReviewResult{Safe: true}
	}

	pg := postgresBackend()

	// Tell the model what storage layer it is actually reviewing, so it stops
	// inventing SQL-injection findings for code that contains no SQL.
	var storeNote string
	if pg {
		storeNote = "This code targets a PostgreSQL store (database/sql). SQL " +
			"injection and unvalidated-input findings are relevant here."
	} else {
		storeNote = "IMPORTANT: this code uses an IN-MEMORY map-based store. " +
			"There is NO SQL and NO database query string anywhere. Do NOT " +
			"report SQL injection for in-memory map access. Type assertions on " +
			"map[string]any are guarded by shared payload helpers; do not report " +
			"them as unvalidated input unless a value is passed to an external " +
			"system."
	}

	prompt := fmt.Sprintf(`You are a Go security reviewer. Review this generated %s for entity %s.

%s

Check ONLY these four issues:
1. SQL injection: string concatenation inside an actual SQL query string
2. Unvalidated input: user data passed to an external system without checking
3. Hardcoded secrets: passwords, tokens, or keys as string literals
4. Ignored errors: a critical error discarded with _ = someErr (NOT the
   intentional _ = w.Write(...) idiom in scaffold handlers)

CODE:
%s

Return ONLY valid JSON, no explanation, no markdown:
{"safe": true, "issues": []}
  or
{"safe": false, "issues": ["description of issue 1", "description of issue 2"]}

JSON:`, taskType, entityName, storeNote, string(data))

	resp, err := AskLLM(prompt)
	if err != nil {
		fmt.Printf("     [review] ⚠️  skipped (AI unavailable): %v\n", err)
		return ReviewResult{Safe: true}
	}

	start := strings.Index(resp, "{")
	end := strings.LastIndex(resp, "}")
	if start == -1 || end == -1 {
		return ReviewResult{Safe: true}
	}

	var raw ReviewResult
	if json.Unmarshal([]byte(resp[start:end+1]), &raw) != nil {
		return ReviewResult{Safe: true}
	}

	return scopeIssues(raw.Issues, pg)
}

// scopeIssues applies backend-aware severity tiers to the model's raw issue
// strings. The model is noisy and frequently reports SQL injection on code with
// no SQL; rather than trust its binary safe/unsafe verdict, we re-classify each
// issue and recompute Safe from the WARN-level survivors.
func scopeIssues(raw []string, pg bool) ReviewResult {
	res := ReviewResult{Issues: raw}
	for _, text := range raw {
		lc := strings.ToLower(text)
		sev := SevWarn

		isSQL := strings.Contains(lc, "sql injection") ||
			strings.Contains(lc, "sql-injection") ||
			strings.Contains(lc, "query string")
		isUnvalidated := strings.Contains(lc, "unvalidated") ||
			strings.Contains(lc, "without validation") ||
			strings.Contains(lc, "without sanitization") ||
			strings.Contains(lc, "type assertion")
		isIgnoredErr := strings.Contains(lc, "ignored error") ||
			strings.Contains(lc, "errors are ignored") ||
			strings.Contains(lc, "discarded with _")
		// The model often "reports" the absence of a problem as an issue line
		// ("does not contain any hardcoded secrets" / "good practice"). Those
		// are not findings at all — drop them entirely.
		isNonFinding := strings.Contains(lc, "does not contain") ||
			strings.Contains(lc, "no hardcoded") ||
			strings.Contains(lc, "good practice") ||
			strings.Contains(lc, "which is good")

		switch {
		case isNonFinding:
			continue // not a finding; drop
		case (isSQL || isUnvalidated) && !pg:
			sev = SevInfo // in-memory backend: no SQL to inject into
		case isIgnoredErr:
			sev = SevInfo // scaffold _ = idiom is intentional
		}
		res.Findings = append(res.Findings, Issue{Text: text, Severity: sev})
	}

	// Safe iff no WARN-level finding survived.
	res.Safe = true
	for _, f := range res.Findings {
		if f.Severity == SevWarn {
			res.Safe = false
		}
	}
	return res
}

// Warnings returns just the WARN-level finding texts (for the repair hint).
func (r ReviewResult) Warnings() []string {
	var out []string
	for _, f := range r.Findings {
		if f.Severity == SevWarn {
			out = append(out, f.Text)
		}
	}
	if len(out) == 0 {
		// Backwards-compat path: a ReviewResult built without Findings (e.g.
		// from a raw unmarshal) falls back to its Issues slice.
		if len(r.Findings) == 0 {
			return r.Issues
		}
	}
	return out
}

// InfoNotes returns the INFO-level finding texts (shown quietly, not as alarms).
func (r ReviewResult) InfoNotes() []string {
	var out []string
	for _, f := range r.Findings {
		if f.Severity == SevInfo {
			out = append(out, f.Text)
		}
	}
	return out
}

// IssueHint formats review issues into a compact hint string suitable for
// injection into a repair prompt's REPAIR HINT field. Only WARN-level issues
// are fed to repair; info-level notes are not worth a repair round.
func IssueHint(issues []string) string {
	if len(issues) == 0 {
		return ""
	}
	return "SECURITY ISSUES FOUND:\n- " + strings.Join(issues, "\n- ")
}
