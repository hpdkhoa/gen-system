package core

import (
	"fmt"

	"github.com/hpdkhoa/gen-system/generator/internal/deps"
)

func ApplyDependencies(path string, profile string) {
	fmt.Println("🔒 Applying dependency profile:", profile)

	if err := deps.WriteFrontendPackage(path, profile); err != nil {
		fmt.Println("❌ frontend deps error:", err)
	}

	if err := deps.WriteMobilePackage(path, profile); err != nil {
		fmt.Println("❌ mobile deps error:", err)
	}

	if err := deps.WriteGoMod(path, profile); err != nil {
		fmt.Println("❌ backend deps error:", err)
	}

}
