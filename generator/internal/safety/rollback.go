package safety

import (
	"io"
	"os"
	"path/filepath"
)

func RestoreSnapshot(root, snapDir string) error {

	err := filepath.Walk(snapDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(snapDir, path)
		target := filepath.Join(root, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyBack(path, target)
	})
	if err != nil {
		return err
	}

	// v26.58 (D099): the copy above gives old files their old content but
	// never lists root, so every file the failed run created stayed, and the
	// caller reported a rollback over a mix of old and new files. A file the
	// snapshot does not hold did not exist when it was taken: remove it. The
	// .gen folder is kept whole; it holds the snapshots and the tool's own
	// state. This runs only after a complete copy, so a missing or unreadable
	// snapshot never empties root.
	genDir := filepath.Join(root, ".gen")
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == genDir {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if _, statErr := os.Lstat(filepath.Join(snapDir, rel)); os.IsNotExist(statErr) {
			return os.Remove(path)
		}
		return nil
	})

}

func copyBack(src, dst string) error {
	os.MkdirAll(filepath.Dir(dst), 0755)

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err

}
