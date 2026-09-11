// heal_log.go — v25.5 (self-healing observability, step 1 / Path A CLI opt-in).
//
// `--heal-log <path>` registers a JSONL agent-event sink so a CLI generation run
// (gen idea / run / continue / evolve / apply) writes one structured event per
// line: every lifecycle milestone and every self-healing step. Two uses:
//  1. Watch/replay what the agent did without scrolling terminal output.
//  2. Capture repair-trace data for the fine-tune pipeline (open item #6),
//     which is explicitly blocked on exactly this data.
//
// Off by default: with no --heal-log flag, no sink is registered and behaviour
// is byte-identical to before. The returned cleanup closes the file; callers
// defer it. Parsing mirrors GetSchemaPath (args.go) — a plain os.Args scan, no
// flag package, consistent with the rest of the CLI.
package cli

import (
	"fmt"
	"os"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
)

// healLogPath returns the value of --heal-log, or "" if absent.
func healLogPath() string {
	for i, arg := range os.Args {
		if arg == "--heal-log" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

// SetupHealLog registers a JSONL agent sink if --heal-log <path> is present and
// returns a cleanup function to close it. When the flag is absent it returns a
// no-op cleanup, so callers can unconditionally `defer SetupHealLog()()`.
func SetupHealLog() func() {
	path := healLogPath()
	if path == "" {
		return func() {}
	}
	sink, err := agent.NewJSONLSink(path)
	if err != nil {
		fmt.Printf("⚠️  --heal-log: could not open %s: %v (continuing without a trace)\n", path, err)
		return func() {}
	}
	agent.AddAgentSink(sink)
	fmt.Printf("📓 heal-log: writing agent trace to %s\n", path)
	return func() { _ = sink.Close() }
}
