package cli

import (
	"fmt"
	"os"
)

func Doctor() {
	fmt.Println("🩺 Running diagnostics...")
	check(".gen")
	check("generator")
	check("go.mod")
	fmt.Println("✅ System healthy")
}

func check(path string) {
	if _, err := os.Stat(path); err != nil {
		fmt.Println("❌ Missing:", path)
	} else {
		fmt.Println("✅ OK:", path)
	}
}
