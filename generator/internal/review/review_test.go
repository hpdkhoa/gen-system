package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

func TestParseDiff(t *testing.T) {
	d := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -3,2 +3,3 @@ func A() {
-	x := 1
-	y := 2
+	x := 3
+	y := 4
+	z := 5
@@ -10,0 +12 @@ func B() {
+	return
diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package p
+func N() {}
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package p
-func G() {}
diff --git a/old.go b/moved.go
similarity index 90%
rename from old.go
rename to moved.go
diff --git a/img.png b/img.png
Binary files a/img.png and b/img.png differ
`
	fs := ParseDiff(d)
	if len(fs) != 5 {
		t.Fatalf("files: got %d, want 5", len(fs))
	}
	a := fs[0]
	if a.OldPath != "a.go" || a.NewPath != "a.go" {
		t.Errorf("paths: %q %q", a.OldPath, a.NewPath)
	}
	if got := a.NewLines; len(got) != 2 || got[0] != (Range{3, 5}) || got[1] != (Range{12, 12}) {
		t.Errorf("new lines: %v", got)
	}
	if got := a.OldLines; len(got) != 1 || got[0] != (Range{3, 4}) {
		t.Errorf("old lines: %v", got)
	}
	if n := fs[1]; !n.Added || n.OldPath != "" || n.NewPath != "new.go" || n.NewLines[0] != (Range{1, 2}) {
		t.Errorf("new file: %+v", n)
	}
	if g := fs[2]; !g.Deleted || g.NewPath != "" || g.OldPath != "gone.go" || g.OldLines[0] != (Range{1, 2}) {
		t.Errorf("deleted file: %+v", g)
	}
	if r := fs[3]; r.OldPath != "old.go" || r.NewPath != "moved.go" || r.Added || r.Deleted {
		t.Errorf("rename: %+v", r)
	}
	if !fs[4].Binary {
		t.Errorf("binary not seen")
	}
}

func TestIsTestFile(t *testing.T) {
	yes := []string{"a/b_test.go", "test_x.py", "pkg/x_test.py", "tests/helpers.py", "src/a.test.ts", "web/b.spec.tsx",
		"src/main/java/FooTest.java", "src/test/java/x/Util.java"}
	no := []string{"a/b.go", "testing.go", "contest.py", "src/a.ts", "src/main/java/Foo.java", "attest.py"}
	for _, p := range yes {
		if !isTestFile(p) {
			t.Errorf("%s should be a test file", p)
		}
	}
	for _, p := range no {
		if isTestFile(p) {
			t.Errorf("%s should not be a test file", p)
		}
	}
}

// repo makes a git repo with the given files committed, then applies the second set to the working tree.
func repo(t *testing.T, before, after map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	dir := t.TempDir()
	write := func(files map[string]string) {
		for p, c := range files {
			full := filepath.Join(dir, filepath.FromSlash(p))
			if c == "" {
				_ = os.Remove(full)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "core.autocrlf=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(before)
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	write(after)
	return dir
}

const storeBefore = `package app

import "os"

func Save(name string) error {
	return nil
}

func Old() int {
	return 1
}

func Helper(n int) int {
	return n + 1
}

func Untouched() {
	_ = os.Getenv("X")
}
`

const storeAfter = `package app

import (
	"net/http"
	"os"
)

func Save(name string, force bool) error {
	return os.WriteFile(name, nil, 0o644)
}

func Helper(n int) int {
	if n > 10 {
		return 10
	}
	if n < 0 {
		return 0
	}
	if n == 5 {
		return 6
	}
	return n + 1
}

func Fresh() error {
	_, err := http.Get("http://example.com")
	return err
}

func Untouched() {
	_ = os.Getenv("X")
}
`

const callers = `package app

func Main() {
	_ = Save("a")
	_ = Old()
}
`

const helperTest = `package app

import "testing"

func TestHelper(t *testing.T) {
	if Helper(1) != 2 {
		t.Fatal("bad")
	}
}
`

func points(r *Report, level string) []string {
	var out []string
	for _, p := range r.Points {
		if p.Level == level {
			out = append(out, p.Text)
		}
	}
	return out
}

func has(list []string, words ...string) bool {
	for _, s := range list {
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(s, w)
		}
		if ok {
			return true
		}
	}
	return false
}

func TestRunGitBase(t *testing.T) {
	dir := repo(t,
		map[string]string{"store.go": storeBefore, "main.go": callers, "helper_test.go": helperTest},
		map[string]string{"store.go": storeAfter})
	ctx := agentctx.LoadContext(dir)
	r, err := Run(ctx.Index, Options{Root: dir, Base: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + Markdown(r))
	br := points(r, "break")
	if !has(br, "Save", "signature", "Main") {
		t.Errorf("want a break for Save's signature with caller Main, got %v", br)
	}
	if !has(br, "Old", "removed", "Main") {
		t.Errorf("want a break for removed Old still called by Main, got %v", br)
	}
	be := points(r, "behavior")
	if !has(be, "Save", "files") {
		t.Errorf("want Save to gain files, got %v", be)
	}
	if !has(be, "Fresh", "network") {
		t.Errorf("want new Fresh to touch the network, got %v", be)
	}
	te := points(r, "test")
	if !has(te, "Save") || !has(te, "Fresh") {
		t.Errorf("want Save and Fresh untested, got %v", te)
	}
	if has(te, "Helper") {
		t.Errorf("Helper is reached by TestHelper, got %v", te)
	}
	if !has(points(r, "size"), "Helper", "3 more branches") {
		t.Errorf("want Helper +3 branches, got %v", points(r, "size"))
	}
	for _, f := range r.Functions {
		if strings.HasSuffix(f.Name, "Untouched") {
			t.Errorf("Untouched did not change but was listed")
		}
	}
	if len(r.Points) != 7 {
		t.Errorf("points: got %d, want 7 (2 break, 2 behavior, 2 test, 1 size): %+v", len(r.Points), r.Points)
	}
}

func TestRunUntrackedAndDiffText(t *testing.T) {
	dir := repo(t, map[string]string{"main.go": "package app\n\nfunc Main() {}\n"},
		map[string]string{"extra.go": "package app\n\nfunc Extra() int {\n\treturn 1\n}\n"})
	ctx := agentctx.LoadContext(dir)
	r, err := Run(ctx.Index, Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Functions) != 1 || r.Functions[0].Status != "added" || !strings.HasSuffix(r.Functions[0].Name, "Extra") {
		t.Fatalf("want the untracked file's Extra as added, got %+v", r.Functions)
	}

	// Diff text without a base: only callers and tests.
	diff := "diff --git a/extra.go b/extra.go\n--- a/extra.go\n+++ b/extra.go\n@@ -4 +4 @@\n-\treturn 1\n+\treturn 2\n"
	r, err = Run(ctx.Index, Options{Root: dir, Diff: diff})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notes) == 0 || len(points(r, "break")) != 0 || len(points(r, "behavior")) != 0 {
		t.Errorf("diff text: want a note and no break or behavior points, got %+v", r)
	}
	if !has(points(r, "test"), "Extra") {
		t.Errorf("diff text: want Extra untested, got %v", r.Points)
	}
}

func TestRunNoChange(t *testing.T) {
	dir := repo(t, map[string]string{"main.go": "package app\n\nfunc Main() {}\n"}, nil)
	ctx := agentctx.LoadContext(dir)
	r, err := Run(ctx.Index, Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 0 || len(r.Points) != 0 || len(r.Functions) != 0 {
		t.Errorf("no change: got %+v", r)
	}
	if !strings.Contains(Markdown(r), "0 files changed") {
		t.Errorf("markdown: %s", Markdown(r))
	}
}
