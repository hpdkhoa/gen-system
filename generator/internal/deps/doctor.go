package deps

import (
	"fmt"
	"os"
)

func Doctor(path string) {

	fmt.Println("🩺 Checking dependency health...")

	if _, err := os.Stat(path + "/frontend/package.json"); err != nil {
		fmt.Println("❌ frontend missing package.json")
	}

	if _, err := os.Stat(path + "/mobile/package.json"); err != nil {
		fmt.Println("❌ mobile missing package.json")
	}

	if _, err := os.Stat(path + "/backend/go.mod"); err != nil {
		fmt.Println("❌ backend missing go.mod")
	}

	fmt.Println("✅ basic checks done")

}
