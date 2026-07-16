// cli/skill.go — TASK 4 (v19): 'gen skill' subcommand.
// Writes a versioned .skill file from the current project state.
//
// Usage:
//
//	gen skill [--root .] [--out .gen/project.skill] [--constraints constraints.txt]
//
// The output file is a JSONL-format SkillFile: one JSON object per line.
// It captures the full symbol graph, call edges, and optional project
// constraints. LoadContext can ingest it as an authoritative context source.
package cli

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	agentctx "gen-system/generator/internal/context"
	"gen-system/generator/internal/objgraph"
)

// WriteSkill implements 'gen skill'. args are the command-line args
// after "skill" (i.e. os.Args[2:]).
func WriteSkill(args []string) {
	fs := flag.NewFlagSet("skill", flag.ExitOnError)
	root := fs.String("root", ".", "Project root to analyse")
	out := fs.String("out", ".gen/project.skill", "Output .skill file path")
	constrFile := fs.String("constraints", "", "Optional file of constraint strings (one per line)")
	_ = fs.Parse(args)

	fmt.Printf("gen skill  root=%s  out=%s\n", *root, *out)

	// Load constraints from file if provided.
	var constraints []string
	if *constrFile != "" {
		var err error
		constraints, err = readLines(*constrFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading constraints file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  constraints: %d lines from %s\n", len(constraints), *constrFile)
	}

	// Full project parse (same path as gen-server start-up).
	fmt.Println("  parsing project …")
	ctx := agentctx.LoadContext(*root)
	fmt.Printf("  parsed  symbols=%d  files=%d\n",
		len(ctx.Index.AllSymbols()), len(ctx.Files))

	// Build ObjectGraph.
	g := objgraph.Build(*root, ctx.Index, ctx.Effects, objgraph.DefaultBuildOptions)
	fmt.Printf("  graph   nodes=%d  edges=%d\n", len(g.Nodes), len(g.Edges))

	// Write skill file.
	if err := objgraph.WriteSkillFile(*out, g, constraints, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error writing skill file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  wrote   %s\n", *out)

	// Quick verification: round-trip read.
	sf, err := objgraph.ReadSkillFile(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not verify written file: %v\n", err)
		return
	}
	fmt.Printf("  verify  nodes=%d  edges=%d  constraints=%d  version=%d\n",
		len(sf.Nodes), len(sf.Edges), len(sf.Constraints), sf.Header.SkillVersion)
	fmt.Println("  ok")
}

// readLines reads a text file and returns non-empty, non-comment lines.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}
