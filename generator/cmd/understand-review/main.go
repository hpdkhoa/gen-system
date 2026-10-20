// generator/cmd/understand-review: reviews a change with parser facts and prints them. No model takes part.
//
//	understand-review --root . --base main            Markdown for a person
//	understand-review --root . --base main --json     JSON for a tool
//	understand-review --root . --fail-on break        exit 1 when a point of that level is found (for CI)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/review"
)

var version = "dev"

func main() {
	root := flag.String("root", ".", "repo root")
	base := flag.String("base", "HEAD", "git revision to compare the working tree with")
	diffFile := flag.String("diff", "", "review this diff file instead of asking git")
	hops := flag.Int("hops", 3, "how many calls up to look for a test")
	asJSON := flag.Bool("json", false, "print JSON")
	failOn := flag.String("fail-on", "", "exit 1 when a point of this level is found: break, behavior, test or size")
	copybooks := flag.String("copybooks", "", "COBOL copybook search path")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("understand-review", version)
		return
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	opt := review.Options{Root: abs, Base: *base, Hops: *hops}
	if *diffFile != "" {
		b, err := os.ReadFile(*diffFile)
		if err != nil {
			fail(err)
		}
		opt.Diff = string(b)
	}
	// Package code prints progress to stdout; keep stdout for the report.
	out := os.Stdout
	os.Stdout = os.Stderr
	lo := agentctx.LoadContextOpts{}
	if *copybooks != "" {
		lo.CopybookPath = filepath.SplitList(*copybooks)
	}
	ctx := agentctx.LoadContextWithOpts(abs, lo)
	rep, err := review.Run(ctx.Index, opt)
	if err != nil {
		fail(err)
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		fmt.Fprint(out, review.Markdown(rep))
	}
	if *failOn != "" {
		for _, p := range rep.Points {
			if p.Level == *failOn {
				os.Exit(1)
			}
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "understand-review:", err)
	os.Exit(2)
}
