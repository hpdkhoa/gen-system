// N01: when a Commit failed and the rollback could not restore a live file,
// fail ignored the restore error, deleted .gen/commit-backup (the only copy of
// the original file) and still said live was left unchanged. The rollback
// also left the empty folders the commit had created in live.
package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaging_FailedRestoreKeepsBackup(t *testing.T) {
	live := t.TempDir()
	stage := filepath.Join(live, stagingDirName)
	writeStagingFile(t, filepath.Join(live, "backend", "keep.go"), "old\n")
	writeStagingFile(t, filepath.Join(stage, "backend", "keep.go"), "new\n")

	keep := filepath.Join(live, "backend", "keep.go")
	orig := restoreStagingFile
	restoreStagingFile = func(src, dst string) error {
		if dst == keep {
			return errors.New("access is denied")
		}
		return orig(src, dst)
	}
	t.Cleanup(func() { restoreStagingFile = orig })

	s := NewStagingService(nil, nil, live)
	s.last = &StageResult{Change: "c", StagingDir: stage, Files: []FileDiff{
		{Path: filepath.Join("backend", "keep.go"), Layer: "backend", Status: "modified"},
		// the staged file is missing, so the commit fails here
		{Path: filepath.Join("backend", "missing.go"), Layer: "backend", Status: "added"},
	}}
	_, err := s.Commit()
	if err == nil {
		t.Fatal("want Commit to fail")
	}
	msg := err.Error()
	if strings.Contains(msg, "live left unchanged") {
		t.Fatalf("error claims live is unchanged although keep.go was not restored: %v", err)
	}
	backup := filepath.Join(live, ".gen", "commit-backup")
	if !strings.Contains(msg, "keep.go") || !strings.Contains(msg, backup) {
		t.Fatalf("error must name keep.go and the backup folder %s, got %v", backup, err)
	}
	if got := readStagingFile(t, filepath.Join(backup, "backend", "keep.go")); got != "old\n" {
		t.Fatalf("backup of keep.go = %q, want the original live content", got)
	}
}

func TestStaging_FailedCommitRemovesCreatedFolders(t *testing.T) {
	live := t.TempDir()
	stage := filepath.Join(live, stagingDirName)
	writeStagingFile(t, filepath.Join(live, "backend", "keep.go"), "old\n")
	writeStagingFile(t, filepath.Join(stage, "backend", "newdir", "deep", "a.go"), "package deep\n")

	s := NewStagingService(nil, nil, live)
	s.last = &StageResult{Change: "c", StagingDir: stage, Files: []FileDiff{
		{Path: filepath.Join("backend", "newdir", "deep", "a.go"), Layer: "backend", Status: "added"},
		// a new folder in a new layer; the staged file is missing, so the commit fails here
		{Path: filepath.Join("mobile", "lib", "missing.dart"), Layer: "mobile", Status: "added"},
	}}
	if _, err := s.Commit(); err == nil {
		t.Fatal("want Commit to fail")
	}
	for _, dir := range []string{
		filepath.Join(live, "backend", "newdir"),
		filepath.Join(live, "mobile"),
	} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("folder %s created by the failed commit is still in live (err %v)", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(live, "backend", "keep.go")); err != nil {
		t.Fatalf("backend folder of the live project was touched: %v", err)
	}
}
