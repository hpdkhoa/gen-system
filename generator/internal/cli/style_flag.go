// style_flag.go — v26.33: `--style <gen-style.json>` and `--style-mode
// <off|report|gate|autofix>` turn on the team style profile for a generation
// run. The same two settings can come from the environment (GEN_STYLE,
// GEN_STYLE_MODE), which is how bench drivers pass them; a flag on the
// command line wins over the environment.
//
// Absent both → agent.SetStyle(nil, "") → every style seam is nil and the run
// is byte-identical to v26.32. Parsing mirrors verify_flag.go (a plain
// os.Args scan), consistent with the rest of the CLI.
package cli

import (
	"fmt"
	"os"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/style"
)

func flagValue(name string) (string, bool) {
	for i, arg := range os.Args {
		if arg == name && i+1 < len(os.Args) && !isFlagish(os.Args[i+1]) {
			return os.Args[i+1], true
		}
	}
	return "", false
}

// SetupStyle reads the flags or the environment, loads the profile, and
// installs it in the agent. Returns the resolved mode ("" when off).
func SetupStyle() string {
	path, ok := flagValue("--style")
	if !ok {
		path = os.Getenv("GEN_STYLE")
	}
	if path == "" {
		agent.SetStyle(nil, "")
		return ""
	}
	p, err := style.Load(path)
	if err != nil {
		fmt.Printf("⚠️  style profile not loaded (%v); running without one\n", err)
		agent.SetStyle(nil, "")
		return ""
	}
	modeStr, ok := flagValue("--style-mode")
	if !ok {
		modeStr = os.Getenv("GEN_STYLE_MODE")
	}
	mode := style.Mode(modeStr)
	switch mode {
	case "", style.ModeOff, style.ModeReport, style.ModeGate, style.ModeAutofix:
	default:
		fmt.Printf("⚠️  unknown --style-mode %q; using the profile's own mode\n", modeStr)
		mode = ""
	}
	agent.SetStyle(p, mode)
	if !agent.StyleActive() {
		fmt.Printf("📐 style: profile %q loaded, mode off\n", p.Name)
		return string(style.ModeOff)
	}
	effective := mode
	if effective == "" {
		effective = p.EffectiveMode()
	}
	fmt.Printf("📐 style: %q (%s), %d rules, mode %s\n", p.Name, p.Source, len(p.ActiveRules()), effective)
	return string(effective)
}
