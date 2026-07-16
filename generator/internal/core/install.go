package core

import (
	"fmt"
	"os/exec"
)

func InstallAll(path string) {
	fmt.Println("📦 Installing dependencies...")

	exec.Command("bash", "-c", "cd "+path+"/backend && go mod tidy").Run()
	exec.Command("bash", "-c", "cd "+path+"/frontend && npm install").Run()
	exec.Command("bash", "-c", "cd "+path+"/mobile && npm install").Run()

	fmt.Println("✅ Install complete")

}
