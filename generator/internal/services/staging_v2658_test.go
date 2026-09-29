// Staging defects of v26.58.
// D092: computeDiff walked only the staging tree, so a file the staged run
// deleted never got the status removed and Commit left it in live.
package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeStagingFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func readStagingFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestStaging_CommitRemovesFileDeletedInStaging(t *testing.T) {
	live := t.TempDir()
	stage := filepath.Join(live, stagingDirName)
	writeStagingFile(t, filepath.Join(live, "backend", "gone.go"), "package x\n")
	writeStagingFile(t, filepath.Join(live, "backend", "keep.go"), "old\n")
	writeStagingFile(t, filepath.Join(stage, "backend", "keep.go"), "new\n")

	s := NewStagingService(nil, nil, live)
	files := s.computeDiff(stage)
	status := map[string]string{}
	for _, f := range files {
		status[filepath.ToSlash(f.Path)] = f.Status
	}
	if status["backend/gone.go"] != "removed" || status["backend/keep.go"] != "modified" || len(files) != 2 {
		t.Fatalf("want gone.go removed and keep.go modified, got %+v", status)
	}

	s.last = &StageResult{Change: "c", StagingDir: stage, Files: files}
	if _, err := s.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(live, "backend", "gone.go")); !os.IsNotExist(err) {
		t.Fatalf("gone.go still in live after commit (err %v)", err)
	}
	if got := readStagingFile(t, filepath.Join(live, "backend", "keep.go")); got != "new\n" {
		t.Fatalf("keep.go = %q, want the staged content", got)
	}
}

// D093: Stage read the IR from .gen/ir_snapshot.json in the working
// directory, not under the root it stages, so gen-server --root planned the
// entities of another project.
func TestStaging_StageReadsIRFromRoot(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no model in tests", http.StatusServiceUnavailable)
	}))
	defer dead.Close()
	t.Setenv("OLLAMA_HOST", dead.URL)

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeStagingFile(t, filepath.Join(cwd, ".gen", "ir_snapshot.json"),
		`{"Entities":[{"Name":"Alpha","Fields":[{"Name":"id","Type":"int"}]}]}`)
	root := t.TempDir() // the staged project has no IR

	s := NewStagingService(nil, nil, root)
	_, err := s.Stage("add a field")
	if err == nil || !strings.Contains(err.Error(), "no IR found") {
		t.Fatalf("want the no IR error for a root without IR, got %v", err)
	}
}

// D094: a failed Stage wiped the staging folder but kept the old proposal,
// so Pending showed it and Commit copied files that no longer matched it.
func TestStaging_FailedStageDropsOldProposal(t *testing.T) {
	t.Chdir(t.TempDir())
	root := t.TempDir() // no IR, so Stage fails at the IR check
	stage := filepath.Join(root, stagingDirName)
	writeStagingFile(t, filepath.Join(stage, "backend", "new.go"), "package x\n")

	s := NewStagingService(nil, nil, root)
	s.last = &StageResult{Change: "A", StagingDir: stage,
		Files: []FileDiff{{Path: filepath.Join("backend", "new.go"), Layer: "backend", Status: "added"}}}
	if _, err := s.Stage("B"); err == nil {
		t.Fatal("want Stage to fail without IR")
	}
	if p := s.Pending(); p != nil {
		t.Fatalf("Pending still returns proposal %q after a failed Stage", p.Change)
	}
	if _, err := s.Commit(); err == nil {
		t.Fatal("want Commit to refuse with nothing staged")
	}
}

// D095: Commit returned on the first copy error with the earlier files
// already in live, so live held a mix of old and staged files.
func TestStaging_FailedCommitLeavesLiveUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, live, stage string) FileDiff
	}{
		{"staged file missing", func(t *testing.T, live, stage string) FileDiff {
			// the staged new.go no longer exists, as after the failed Stage of D094
			return FileDiff{Path: filepath.Join("backend", "new.go"), Layer: "backend", Status: "added"}
		}},
		{"live path blocked", func(t *testing.T, live, stage string) FileDiff {
			// live has a file where the staged file needs a folder
			writeStagingFile(t, filepath.Join(live, "backend", "sub"), "a file\n")
			writeStagingFile(t, filepath.Join(stage, "backend", "sub", "y.go"), "package sub\n")
			return FileDiff{Path: filepath.Join("backend", "sub", "y.go"), Layer: "backend", Status: "added"}
		}},
		{"live file locked", func(t *testing.T, live, stage string) FileDiff {
			if runtime.GOOS != "windows" {
				t.Skip("an open file blocks a delete only on Windows")
			}
			// the staged run deleted lock.go, but a process holds it open in live
			writeStagingFile(t, filepath.Join(live, "backend", "lock.go"), "locked\n")
			h, err := os.Open(filepath.Join(live, "backend", "lock.go"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { h.Close() })
			return FileDiff{Path: filepath.Join("backend", "lock.go"), Layer: "backend", Status: "removed"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			live := t.TempDir()
			stage := filepath.Join(live, stagingDirName)
			writeStagingFile(t, filepath.Join(live, "backend", "gone.go"), "package gone\n")
			writeStagingFile(t, filepath.Join(live, "backend", "keep.go"), "old\n")
			writeStagingFile(t, filepath.Join(stage, "backend", "keep.go"), "new\n")
			writeStagingFile(t, filepath.Join(stage, "backend", "add.go"), "added\n")
			failing := c.setup(t, live, stage)

			s := NewStagingService(nil, nil, live)
			s.last = &StageResult{Change: "c", StagingDir: stage, Files: []FileDiff{
				{Path: filepath.Join("backend", "gone.go"), Layer: "backend", Status: "removed"},
				{Path: filepath.Join("backend", "keep.go"), Layer: "backend", Status: "modified"},
				{Path: filepath.Join("backend", "add.go"), Layer: "backend", Status: "added"},
				failing,
			}}
			if _, err := s.Commit(); err == nil {
				t.Fatal("want Commit to fail")
			}
			if got := readStagingFile(t, filepath.Join(live, "backend", "keep.go")); got != "old\n" {
				t.Fatalf("keep.go = %q after a failed commit, want the live content", got)
			}
			if got := readStagingFile(t, filepath.Join(live, "backend", "gone.go")); got != "package gone\n" {
				t.Fatalf("gone.go = %q after a failed commit", got)
			}
			if _, err := os.Stat(filepath.Join(live, "backend", "add.go")); !os.IsNotExist(err) {
				t.Fatalf("add.go reached live in a failed commit (err %v)", err)
			}
			if _, err := os.Stat(filepath.Join(live, ".gen", "commit-backup")); !os.IsNotExist(err) {
				t.Fatalf("backup folder left behind (err %v)", err)
			}
		})
	}
}
