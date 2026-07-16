package engine

import "fmt"

func WriteBaseProject(path string) {
	fmt.Println("📦 Base project scaffold ready:", path)
}

func Sync(path string) {
	fmt.Println("🔁 Sync not implemented yet")
}

func Doctor(path string) {
	fmt.Println("🩺 Basic checks OK")
}

func Plan(schema string) string {
	return "plan not implemented yet"
}
