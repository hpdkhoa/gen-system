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

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// skip snapshots folder itself
		if strings.Contains(path, ".gen/snapshots") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		target := filepath.Join(snapDir, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target)
	})

	return snapDir, err

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
