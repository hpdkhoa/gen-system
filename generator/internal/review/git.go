package review

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// git runs one read-only git command in root and returns its output.
func git(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// changesSince lists every change in the working tree against base, untracked files included.
func changesSince(root, base string) ([]*FileDiff, error) {
	out, err := git(root, "diff", "--no-color", "--no-ext-diff", "-M", "--unified=0", base, "--")
	if err != nil {
		return nil, err
	}
	files := ParseDiff(string(out))
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(string(untracked), "\x00") {
		if p != "" {
			files = append(files, AddedFile(p, -1))
		}
	}
	return files, nil
}

// showAt returns a file's content at a revision.
func showAt(root, rev, path string) ([]byte, error) {
	return git(root, "show", rev+":"+path)
}
