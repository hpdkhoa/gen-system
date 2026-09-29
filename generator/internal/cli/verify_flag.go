// verify_flag.go — v25.5 (step 2 CLI opt-in): `--verify-understand
// <stop|autofix>` turns on the Understand-validates-Generate pass and picks the
// behaviour on a finding.
//
//	stop     (default when the flag is present without a value): report every
//	         finding, halt for human review, and — for a staged continue — write
//	         a findings report next to the .evolve-review dir.
//	autofix  route fixable findings through the repair loop, then re-report.
//
// Absent flag → VerifyOff → byte-identical to before. Parsing mirrors
// GetSchemaPath / healLogPath (a plain os.Args scan), consistent with the CLI.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
)

// verifyUnderstandValue returns the --verify-understand value. Supports
// "--verify-understand stop", "--verify-understand=stop" and a bare
// "--verify-understand" (defaults to "stop"). Returns ("", false) when the
// flag is absent.
func verifyUnderstandValue() (string, bool) {
	for i, arg := range os.Args {
		if arg == "--verify-understand" {
			if i+1 < len(os.Args) && !isFlagish(os.Args[i+1]) {
				return os.Args[i+1], true
			}
			return "stop", true // bare flag defaults to stop
		}
		// v26.58 (D024): accept "--verify-understand=stop" as GetFlag accepts
		// the "=" form of other flags. Before, the word did not match, the flag
		// counted as absent and the pass stayed off. An empty value after "="
		// means the bare flag, so it defaults to stop too.
		if v, ok := strings.CutPrefix(arg, "--verify-understand="); ok {
			if v == "" {
				return "stop", true
			}
			return v, true
		}
	}
	return "", false
}

// isFlagish reports whether a token looks like the next flag (so a bare
// --verify-understand isn't mis-parsed as taking the following flag as a value).
func isFlagish(s string) bool {
	return len(s) >= 2 && s[0] == '-' && s[1] == '-'
}

// SetupVerifyUnderstand reads the flag and sets agent.VerifyUnderstandMode.
// It returns the resolved mode string for use by the caller (e.g. to decide
// whether to write a findings report after the run).
func SetupVerifyUnderstand() string {
	val, present := verifyUnderstandValue()
	if !present {
		agent.VerifyUnderstandMode = agent.VerifyOff
		return ""
	}
	mode, err := agent.ParseVerifyMode(val)
	if err != nil {
		// v26.58 (D021): a typo used to turn the pass off without a word. Stop
		// with a usage error instead, as the other CLI usage errors do.
		fmt.Printf("❌ %v\n", err)
		os.Exit(2)
	}
	agent.VerifyUnderstandMode = mode
	switch agent.VerifyUnderstandMode {
	case agent.VerifyAutofix:
		fmt.Println("🔎 verify-understand: AUTOFIX — findings route to the repair loop")
	case agent.VerifyStop:
		fmt.Println("🔎 verify-understand: STOP — findings halt for human review")
	}
	return val
}

// WriteVerifyFindingsIfHalted writes a findings report next to reviewDir when the
// last run halted (stop mode with findings). reviewDir is typically the staged
// .evolve-review directory; the report lands as <reviewDir>-FINDINGS.txt so it
// sits alongside the staged code the human is about to inspect. No-op when the
// run did not halt.
func WriteVerifyFindingsIfHalted(reviewDir string) {
	if !agent.VerifyHalted() {
		return
	}
	report := agent.WriteFindingsReport(agent.LastVerifyReport)
	out := reviewDir + "-FINDINGS.txt"
	if reviewDir == "" {
		out = filepath.Join(".", "VERIFY-FINDINGS.txt")
	}
	if err := os.WriteFile(out, []byte(report), 0644); err != nil {
		fmt.Printf("⚠️  could not write findings report: %v\n", err)
		return
	}
	fmt.Printf("\n⛔ Understand-verify halted for review. Findings written to:\n   %s\n", out)
	fmt.Printf("   The staged output was left untouched for your inspection.\n")
}
