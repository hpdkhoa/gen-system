// generator/internal/agent/style_stage.go — v26.33: the team style profile
// inside one generation run.
//
// The profile reaches the model at three moments and the code at two:
//
//	prompt   ai.StyleAddendum adds the rules and reference code to every
//	         logic prompt and logic repair prompt (report, gate, autofix)
//	op gate  engine.StyleGate checks each compiled operation body and sends
//	         violations back to the model under its own cap (gate, autofix)
//	tree     runStyleStage checks the generated backend after the Understand
//	         verify pass; report prints, gate halts on error findings, autofix
//	         routes fixable files through the repair loop once and re-checks
//	heal     healer.go puts the rules into every repair prompt
//
// Off means every seam is nil and the run is byte-identical to v26.32.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/ai"
	agentctx "gen-system/generator/internal/context"
	"gen-system/generator/internal/engine"
	"gen-system/generator/internal/style"
)

var (
	styleProfile *style.Profile
	styleMode    style.Mode = style.ModeOff

	// LastStyleReport holds the most recent tree gate result so the CLI and
	// genbench can read it after RunFromPlan returns.
	LastStyleReport style.Report
	styleHalted     bool
)

// StyleHalted reports whether the last RunFromPlan stopped on style errors.
func StyleHalted() bool { return styleHalted }

// StyleActive reports whether a profile is set and the mode is not off.
func StyleActive() bool { return styleProfile != nil && styleMode != style.ModeOff }

// SetStyle installs a profile for following runs. mode overrides the
// profile's own gate.mode when non-empty. Passing nil turns everything off.
func SetStyle(p *style.Profile, mode style.Mode) {
	styleProfile = p
	styleMode = style.ModeOff
	ai.StyleAddendum = nil
	engine.StyleGate = nil
	engine.StyleMaxRepairs = 1
	if p == nil {
		return
	}
	if mode == "" {
		mode = p.EffectiveMode()
	}
	styleMode = mode
	if mode == style.ModeOff {
		return
	}
	ai.StyleAddendum = func(entity string, hints []string) string {
		block := style.PromptBlock(p)
		if p.Prompt.Exemplars > 0 {
			block += style.RenderExemplars(style.Exemplars(p, hints, "service", p.Prompt.Exemplars))
		}
		return block
	}
	if mode == style.ModeGate || mode == style.ModeAutofix {
		engine.StyleGate = func(name, body string, helpers []string) string {
			return style.RenderFindings(style.CheckOperation(p, name, body, helpers))
		}
		engine.StyleMaxRepairs = p.MaxRepairs()
	}
}

// stylePromptBlock is what the healer adds to repair prompts.
func stylePromptBlock() string {
	if !StyleActive() {
		return ""
	}
	return style.PromptBlock(styleProfile)
}

// runStyleStage checks the generated backend against the profile and acts
// per mode. Returns the report and whether the run should halt.
func runStyleStage(backendPath, rootPath string, ctx agentctx.Context) (style.Report, bool) {
	if !StyleActive() {
		return style.Report{}, false
	}
	emit("style", fmt.Sprintf("\n📐 Style gate (%s mode) — checking generated output against %q...\n", styleMode, styleProfile.Name))
	rep := style.Check(backendPath, styleProfile)
	recordLifecycle("style", fmt.Sprintf("%d error, %d warn over %d rules", rep.Errors(), rep.Warnings(), len(rep.RulesChecked)))
	printStyleFindings(rep)

	if styleMode == style.ModeAutofix && len(rep.FixableFiles()) > 0 {
		emit("style", "  [style] autofix mode — routing fixable findings to the repair loop\n")
		var files []string
		for _, f := range rep.FixableFiles() {
			files = append(files, filepath.Join(backendPath, filepath.FromSlash(f)))
		}
		dispatchRepairs(files, ctx, rootPath, 98) // round 98 = style-driven
		after := style.Check(backendPath, styleProfile)
		emit("style", fmt.Sprintf("  [style] after repair: %d error, %d warn remain\n", after.Errors(), after.Warnings()))
		recordLifecycle("style", fmt.Sprintf("autofix done: %d→%d findings", len(rep.Findings), len(after.Findings)))
		rep = after
	}

	switch {
	case len(rep.Findings) == 0:
		emit("style", "  [style] ✅ generated output follows the team profile\n")
		return rep, false
	case styleMode == style.ModeReport:
		return rep, false
	case rep.Blocking():
		emit("style", fmt.Sprintf("  [style] ⛔ %d error finding(s); leaving output for human review\n", rep.Errors()))
		out := filepath.Join(rootPath, "STYLE-FINDINGS.txt")
		if err := os.WriteFile(out, []byte(style.RenderReport(rep)), 0o644); err == nil {
			emit("style", "  [style] findings written to "+out+"\n")
		}
		recordLifecycle("style", "halted for human review")
		return rep, true
	}
	return rep, false
}

func printStyleFindings(rep style.Report) {
	for _, f := range rep.Findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		emit("style", fmt.Sprintf("  [style] %s %s  %s  %s\n", strings.ToUpper(string(f.Severity)), f.Rule, loc, f.Detail))
	}
}
