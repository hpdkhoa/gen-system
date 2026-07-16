package cli

import (
	"os"
	"path/filepath"
)

type RepoContext struct {
	HasBackend  bool
	HasFrontend bool
}

func DetectRepo(path string) RepoContext {

	ctx := RepoContext{}

	if _, err := os.Stat(filepath.Join(path, "backend")); err == nil {
		ctx.HasBackend = true
	}

	if _, err := os.Stat(filepath.Join(path, "frontend")); err == nil {
		ctx.HasFrontend = true
	}

	return ctx

}
