// generator/internal/cli/outdir.go — v23.19: unified, safe output-directory
// policy for every generating command. Core rules (per project instruction):
//  1. NEVER write generated output into the gen-system source tree or silently
//     into the current directory. New generation goes to a clear, reviewable
//     folder so a human can inspect it before trusting it.
//  2. EVOLVING existing code stages changes to a separate review dir; the user
//     applies them to the real source only after reviewing.
//  3. NO overwrite without notice: if the resolved output dir already contains a
//     generated app, warn and require confirmation (or --force) before writing.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetFlag returns the value following --name in os.Args, or def if absent.
func GetFlag(name, def string) string {
	want := "--" + name
	for i, a := range os.Args {
		if a == want && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(a, want+"=") {
			return strings.TrimPrefix(a, want+"=")
		}
	}
	return def
}

// HasFlag reports whether a bare boolean flag (e.g. --force, --yes) is present.
func HasFlag(name string) bool {
	want := "--" + name
	for _, a := range os.Args {
		if a == want {
			return true
		}
	}
	return false
}

// looksLikeGenSourceTree reports whether dir is (or is inside) the gen-system
// source checkout — the one place generated output must never land. Heuristic:
// a go.mod whose module is gen-system, or a generator/internal directory.
func looksLikeGenSourceTree(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	// Walk up a few levels looking for the gen-system module marker.
	cur := abs
	for i := 0; i < 6; i++ {
		if data, err := os.ReadFile(filepath.Join(cur, "go.mod")); err == nil {
			if strings.Contains(string(data), "module gen-system") {
				return true
			}
		}
		if st, err := os.Stat(filepath.Join(cur, "generator", "internal", "engine")); err == nil && st.IsDir() {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return false
}

// hasGeneratedApp reports whether dir already contains a generated backend (so
// writing again would overwrite). Cheap check: backend/main.go or backend/go.mod.
func hasGeneratedApp(dir string) bool {
	for _, p := range []string{
		filepath.Join(dir, "backend", "main.go"),
		filepath.Join(dir, "backend", "go.mod"),
		filepath.Join(dir, "backend", "internal", "model"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// OutDirOptions configures ResolveOutDir per command.
type OutDirOptions struct {
	// Flag is the chosen --out value ("" if the user gave none).
	Flag string
	// DefaultName is the directory used when the user gave no --out
	// (e.g. "generated-app" for `gen idea`, "output" for `gen apply`).
	DefaultName string
	// Command is the command name, used in messages (e.g. "idea").
	Command string
	// Force bypasses the overwrite confirmation (from --force).
	Force bool
	// AssumeYes auto-confirms prompts (from --yes), for non-interactive use.
	AssumeYes bool
}

// ResolveOutDir applies the output-safety policy and returns the directory the
// agent should write into, or ("", false) if the command must abort.
//
// Policy:
//   - If --out names the gen-system source tree → REFUSE (forces a real choice).
//   - If no --out was given → use ./<DefaultName>, never the bare cwd, and never
//     the source tree; tell the user exactly where output is going.
//   - If the resolved dir already holds a generated app → warn and require
//     confirmation (or --force / --yes) before overwriting.
func ResolveOutDir(o OutDirOptions) (string, bool) {
	dir := strings.TrimSpace(o.Flag)
	explicit := dir != ""

	if !explicit {
		dir = o.DefaultName
		fmt.Printf("📁 No --out given; writing generated output to ./%s/ (use --out <dir> to choose)\n", dir)
	}

	// Rule 1: never write into the gen-system source tree.
	if looksLikeGenSourceTree(dir) {
		fmt.Printf("⛔ Refusing to generate into the gen-system source tree (%s).\n", dir)
		fmt.Printf("   Pick an output dir outside the source, e.g.:\n")
		fmt.Printf("     gen %s --out ~/apps/my-app ...\n", o.Command)
		return "", false
	}

	abs, _ := filepath.Abs(dir)

	// Rule 3: no overwrite without notice.
	if hasGeneratedApp(dir) {
		fmt.Printf("⚠️  %s already contains a generated app.\n", abs)
		if !o.Force && !o.AssumeYes {
			if !confirm(fmt.Sprintf("   Overwrite the generated files in %s?", abs)) {
				fmt.Println("   Aborted. Use --out <new-dir> to generate elsewhere, or --force to overwrite.")
				return "", false
			}
		} else {
			fmt.Println("   --force/--yes given; overwriting.")
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Printf("❌ Could not create output dir %s: %s\n", dir, err)
		return "", false
	}
	fmt.Printf("📂 Output directory: %s\n", abs)
	return dir, true
}

// confirm asks a yes/no question on stdin. Defaults to NO on EOF/empty so a
// non-interactive run never silently overwrites.
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// ResolveEvolveTarget implements the evolve safety rule: evolving existing code
// must NOT overwrite the source in place unless the user explicitly opts in with
// --apply. By default it stages into a review directory (a copy of the target),
// so the user can diff and verify before applying.
//
// Returns (writeDir, inPlace, ok):
//   - inPlace == true  → user passed --apply; writeDir == target (overwrite-guarded by caller intent).
//   - inPlace == false → writeDir is a staging copy of target for review.
func ResolveEvolveTarget(target string) (string, bool, bool) {
	if strings.TrimSpace(target) == "" {
		fmt.Println("❌ evolve needs a target path")
		return "", false, false
	}
	if looksLikeGenSourceTree(target) {
		fmt.Printf("⛔ Refusing to evolve the gen-system source tree (%s).\n", target)
		return "", false, false
	}

	apply := HasFlag("apply")
	reviewDir := GetFlag("review-dir", "")

	if apply {
		// In-place evolution — explicit opt-in. Caller already loaded the
		// target; we only confirm the destructive intent unless --force/--yes.
		abs, _ := filepath.Abs(target)
		if !HasFlag("force") && !HasFlag("yes") {
			fmt.Printf("⚠️  --apply will modify the existing source at %s in place.\n", abs)
			if !confirm("   Proceed with in-place evolution?") {
				fmt.Println("   Aborted. Omit --apply to stage changes into a review dir instead.")
				return "", false, false
			}
		}
		return target, true, true
	}

	// Default: stage into a review dir (copy of the target).
	if reviewDir == "" {
		reviewDir = strings.TrimRight(target, "/\\") + ".evolve-review"
	}
	if looksLikeGenSourceTree(reviewDir) {
		fmt.Printf("⛔ Review dir resolves into the source tree (%s); choose another with --review-dir.\n", reviewDir)
		return "", false, false
	}
	// Start the review dir as a fresh copy of the target so the diff is meaningful.
	if err := os.RemoveAll(reviewDir); err != nil {
		fmt.Printf("❌ Could not reset review dir %s: %s\n", reviewDir, err)
		return "", false, false
	}
	if err := copyDirShallowTree(target, reviewDir); err != nil {
		fmt.Printf("⚠️  Could not seed review dir from target (%s); starting empty.\n", err)
		os.MkdirAll(reviewDir, 0o755)
	}
	abs, _ := filepath.Abs(reviewDir)
	fmt.Printf("🧬 Staging evolution into review dir (source untouched): %s\n", abs)
	fmt.Printf("   Review the changes there, then apply with:\n")
	fmt.Printf("     diff -ru %s %s        # inspect\n", target, reviewDir)
	fmt.Printf("     gen evolve %s --schema <f> --apply   # write in place once satisfied\n", target)
	return reviewDir, false, true
}

// copyDirShallowTree copies a directory tree (files + dirs), skipping VCS and
// common build/output noise, so a review dir starts as a faithful copy of the
// target without dragging huge caches.
func copyDirShallowTree(src, dst string) error {
	skip := map[string]bool{".git": true, "node_modules": true, ".gen": true, "dist": true}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) > 0 && skip[parts[0]] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
