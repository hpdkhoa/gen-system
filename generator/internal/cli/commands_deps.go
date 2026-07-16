package cli

import (
	"fmt"
	"os"

	"gen-system/generator/internal/core"
)

func Deps() {
	if len(os.Args) < 4 {
		fmt.Println("gen deps <repo-path> <profile>")
		return
	}

	path := os.Args[2]
	profile := os.Args[3]

	core.ApplyDependencies(path, profile)

}
