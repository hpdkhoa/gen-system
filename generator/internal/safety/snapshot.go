package safety

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func SnapshotProject(root string) (string, error) {

	ts := time.Now().Format("20060102_150405")
	snapDir := filepath.Join(root, ".gen", "snapshots", ts)

	// v26.57 (D015): compare the path relative to root with the OS separator,
	// and skip the whole folder. The old test, strings.Contains(path,
	// ".gen/snapshots"), never matched the backslash paths filepath.Walk gives
	// on Windows, so the walk copied the snapshot being written into itself
	// and nested until the path was too long; RunFromPlan then ran with no
	// rollback. On other systems it skipped the entries one by one but still
	// walked every old snapshot.
	snapshotsRel := filepath.Join(".gen", "snapshots")

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == snapshotsRel || strings.HasPrefix(rel, snapshotsRel+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// v26.59 (N02): version control and dependency folders belong to git
		// and the package manager, not to the snapshot. Copying them made a
		// restore put back old refs and packages and delete newer objects.
		if info.IsDir() && path != root && isForeignDir(info.Name()) {
			return filepath.SkipDir
		}

		target := filepath.Join(snapDir, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target)
	})

	return snapDir, err

}

// isForeignDir reports a folder that neither the snapshot nor the restore
// owns, at any depth: .git and node_modules.
func isForeignDir(name string) bool {
	return name == ".git" || name == "node_modules"
}

func copyFile(src, dst string) error {
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
