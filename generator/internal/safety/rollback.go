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

		// v26.59 (N02): an older snapshot may still hold .git or
		// node_modules; never copy them over the newer ones.
		if info.IsDir() && path != snapDir && isForeignDir(info.Name()) {
			return filepath.SkipDir
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
	//
	// v26.59 (N02): .git and node_modules at any depth are skipped too; they
	// belong to git and the package manager, so the snapshot does not hold
	// them and removing their files lost commits and installed packages.
	// Folders the snapshot does not hold are collected and, after the walk,
	// removed deepest first when empty, so the run leaves no empty folders.
	genDir := filepath.Join(root, ".gen")
	var newDirs []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == genDir {
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		if info.IsDir() && isForeignDir(info.Name()) {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		_, statErr := os.Lstat(filepath.Join(snapDir, rel))
		if !os.IsNotExist(statErr) {
			return nil
		}
		if info.IsDir() {
			newDirs = append(newDirs, path)
			return nil
		}
		return os.Remove(path)
	})
	if err != nil {
		return err
	}
	// Walk lists a folder before its contents, so the reverse order removes
	// children first. A folder that still holds a skipped folder stays.
	for i := len(newDirs) - 1; i >= 0; i-- {
		if entries, readErr := os.ReadDir(newDirs[i]); readErr == nil && len(entries) == 0 {
			if rmErr := os.Remove(newDirs[i]); rmErr != nil {
				return rmErr
			}
		}
	}
	return nil

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
