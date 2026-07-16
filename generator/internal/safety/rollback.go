package safety

import (
	"io"
	"os"
	"path/filepath"
)

func RestoreSnapshot(root, snapDir string) error {

	return filepath.Walk(snapDir, func(path string, info os.FileInfo, err error) error {
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
