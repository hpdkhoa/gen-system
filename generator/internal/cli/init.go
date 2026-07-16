package cli

import (
	"fmt"
	"os"
)

func Init() {
	fmt.Println("🚀 Initializing project...")

	os.MkdirAll(".gen", 0755)
	os.MkdirAll("output/frontend", 0755)
	os.MkdirAll("output/mobile", 0755)

	fmt.Println("✅ Project initialized")
}
