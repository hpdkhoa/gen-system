// commands_style.go — v26.33: `gen style` subcommands.
//
//	gen style print                       the default profile, as JSON
//	gen style derive <repo> [--out f]     measure a repository, write a profile
//	gen style check <root> [--profile f]  run the gate on a tree, exit 1 on errors
//
// derive and check read the tree through the Understand engine, so what they
// see is the same symbol graph the rest of the product uses. No model.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/style"
)

// Style dispatches `gen style ...`.
func Style(args []string) {
	if len(args) == 0 {
		styleUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "print":
		raw, _ := json.MarshalIndent(style.Default(), "", "  ")
		fmt.Println(string(raw))
	case "derive":
		styleDerive(args[1:])
	case "check":
		styleCheck(args[1:])
	default:
		fmt.Println("Unknown style command:", args[0])
		styleUsage()
		os.Exit(2)
	}
}

func styleUsage() {
	fmt.Print(`gen style print                        default profile as JSON (edit it, save as gen-style.json)
gen style derive <repo> [--out f]      measure a repository through the Understand graph and write a profile
                                       (default out: <repo>/gen-style.json; source is marked derived)
gen style check <root> [--profile f]   run the gate on a Go tree; --json for machine output; exit 1 on error findings
                                       (default profile: gen-style.json in <root>, else the built-in default)

Use a profile on a run:  gen idea --style gen-style.json --style-mode gate "..."
Environment equivalents: GEN_STYLE, GEN_STYLE_MODE
`)
}

func argAfter(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasArg(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func firstPositional(args []string) string {
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "--out" || a == "--profile" {
			skip = true
			continue
		}
		if !isFlagish(a) {
			return a
		}
	}
	return ""
}

func styleDerive(args []string) {
	repo := firstPositional(args)
	if repo == "" {
		fmt.Println("gen style derive: repository path is required")
		os.Exit(2)
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		fmt.Println("gen style derive:", err)
		os.Exit(2)
	}
	out := argAfter(args, "--out")
	if out == "" {
		out = filepath.Join(abs, "gen-style.json")
	}
	p := style.Derive(abs)
	if err := p.Save(out); err != nil {
		fmt.Println("gen style derive: write:", err)
		os.Exit(1)
	}
	fmt.Printf("📐 derived %d rules from %s\n", len(p.ActiveRules()), abs)
	for _, rule := range p.ActiveRules() {
		ev := p.Evidence[rule]
		if ev.Seen > 0 {
			fmt.Printf("   %-32s seen %d, agree %d %s\n", rule, ev.Seen, ev.Agree, ev.Note)
		} else {
			fmt.Printf("   %-32s\n", rule)
		}
	}
	fmt.Printf("written: %s (source %q; edit it, then set source to \"human\")\n", out, p.Source)
}

func styleCheck(args []string) {
	root := firstPositional(args)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Println("gen style check:", err)
		os.Exit(2)
	}
	var p *style.Profile
	if pf := argAfter(args, "--profile"); pf != "" {
		p, err = style.Load(pf)
	} else if _, statErr := os.Stat(filepath.Join(abs, "gen-style.json")); statErr == nil {
		p, err = style.Load(filepath.Join(abs, "gen-style.json"))
	} else {
		p = style.Default()
	}
	if err != nil {
		fmt.Println("gen style check:", err)
		os.Exit(2)
	}
	rep := style.Check(abs, p)
	if hasArg(args, "--json") {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Print(style.RenderReport(rep))
	}
	if rep.Blocking() {
		os.Exit(1)
	}
}
