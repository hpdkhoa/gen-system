package cli

import (
	"fmt"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func RunWithContext(path string, s schema.Entity) {

	ctx := DetectRepo(path)
	fmt.Println("🔍 Repo context:", ctx)

	entities := []schema.Entity{
		{
			Name:   s.Name,
			Fields: s.Fields,
		},
	}

	engine.RunAll(entities, path)

}
