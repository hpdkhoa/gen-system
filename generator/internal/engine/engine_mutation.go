package engine

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func mutateHandler(s schema.Entity, path string) {

	file := filepath.Join(path, "backend/internal/handler/"+s.Name+"_handler.go")

	full := fmt.Sprintf(`


func GetAll%[1]s(w http.ResponseWriter, r *http.Request) {
data, err := service.GetAll%[1]s()
if err != nil {
http.Error(w, err.Error(), 500)
return
}
json.NewEncoder(w).Encode(data)
}
`, s.Name)

	body := fmt.Sprintf(`


data, err := service.GetAll%[1]s()
if err != nil {
http.Error(w, err.Error(), 500)
return
}
json.NewEncoder(w).Encode(data)
`, s.Name)

	ast.UpsertFunction(file, "GetAll"+s.Name, full, body)

}

func mutateRoutes(s schema.Entity, path string) {

	file := filepath.Join(path, "backend/internal/router/router.go")

	route := fmt.Sprintf(`http.HandleFunc("/%s", handler.GetAll%s)`, s.Name, s.Name)

	ast.UpsertRoute(file, route, "")

}

func RunMutationLayer(s schema.Entity, path string) {
	fmt.Println("🧬 Running mutation layer...")
	mutateHandler(s, path)
	mutateRoutes(s, path)
	validateGoBuild(path)
}

// --------------------------------------------------
// Post-mutation validation (prevents silent corruption)
// --------------------------------------------------
func validateGoBuild(path string) {
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = path

	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("❌ go vet failed:")
		fmt.Println(string(out))
	} else {
		fmt.Println("✅ go vet passed")
	}
}
