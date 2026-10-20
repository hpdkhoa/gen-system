// Package review turns a code change into parser facts a reviewer needs: what changed, who still calls it, what it
// now touches, and whether any test reaches it. No model takes part.
package review

import (
	"bufio"
	"strconv"
	"strings"
)

// Range is a run of lines, 1-based and inclusive.
type Range struct{ Start, End int }

// FileDiff is one file in a unified diff.
type FileDiff struct {
	OldPath, NewPath string // slash paths relative to the repo root; "" when the file is new or deleted
	Added, Deleted   bool
	Binary           bool
	OldLines         []Range // lines removed or changed in the old file
	NewLines         []Range // lines added or changed in the new file
}

// ParseDiff reads a unified diff (git diff output, any context size).
func ParseDiff(text string) []*FileDiff {
	var out []*FileDiff
	var cur *FileDiff
	oldLine, newLine := 0, 0
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur = &FileDiff{}
			out = append(out, cur)
			if a, b, ok := splitGitHeader(line[len("diff --git "):]); ok {
				cur.OldPath, cur.NewPath = a, b
			}
		case cur == nil:
			// text before the first file header
		case strings.HasPrefix(line, "new file mode"):
			cur.Added = true
		case strings.HasPrefix(line, "deleted file mode"):
			cur.Deleted = true
		case strings.HasPrefix(line, "rename from "):
			cur.OldPath = unquote(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			cur.NewPath = unquote(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "Binary files "):
			cur.Binary = true
		case strings.HasPrefix(line, "--- "):
			if p := diffPath(line[4:], "a/"); p == "" {
				cur.Added = true
			} else {
				cur.OldPath = p
			}
		case strings.HasPrefix(line, "+++ "):
			if p := diffPath(line[4:], "b/"); p == "" {
				cur.Deleted = true
			} else {
				cur.NewPath = p
			}
		case strings.HasPrefix(line, "@@"):
			oldLine, newLine = hunkStarts(line)
		case strings.HasPrefix(line, "+"):
			cur.NewLines = addLine(cur.NewLines, newLine)
			newLine++
		case strings.HasPrefix(line, "-"):
			cur.OldLines = addLine(cur.OldLines, oldLine)
			oldLine++
		case strings.HasPrefix(line, " "):
			oldLine++
			newLine++
		}
	}
	for _, f := range out {
		if f.Added {
			f.OldPath = ""
		}
		if f.Deleted {
			f.NewPath = ""
		}
	}
	return out
}

// AddedFile returns a FileDiff for a file git does not track yet: every line is new.
func AddedFile(path string, lines int) *FileDiff {
	f := &FileDiff{NewPath: path, Added: true}
	if lines > 0 {
		f.NewLines = []Range{{1, lines}}
	}
	return f
}

func addLine(rs []Range, n int) []Range {
	if n <= 0 {
		return rs
	}
	if k := len(rs); k > 0 && rs[k-1].End+1 >= n {
		if n > rs[k-1].End {
			rs[k-1].End = n
		}
		return rs
	}
	return append(rs, Range{n, n})
}

// hunkStarts reads "@@ -a,b +c,d @@" and returns a and c.
func hunkStarts(line string) (int, int) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return 0, 0
	}
	return startOf(f[1]), startOf(f[2])
}

func startOf(s string) int {
	s = strings.TrimLeft(s, "-+")
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}

func diffPath(s, prefix string) string {
	s = unquote(strings.TrimSpace(strings.SplitN(s, "\t", 2)[0]))
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// splitGitHeader splits "a/x b/y" into x and y; the --- and +++ lines refine it.
func splitGitHeader(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		return "", "", false
	}
	i := strings.Index(s, " b/")
	if i < 0 || !strings.HasPrefix(s, "a/") {
		return "", "", false
	}
	return s[2:i], s[i+3:], true
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// overlaps reports whether lines a..b touch any range.
func overlaps(rs []Range, a, b int) bool {
	for _, r := range rs {
		if r.Start <= b && a <= r.End {
			return true
		}
	}
	return false
}
