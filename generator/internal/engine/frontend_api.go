package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genAPI(s schema.Entity, path string) {
	file := filepath.Join(path, "frontend/src/api/"+strings.ToLower(s.Name)+".js")

	content := fmt.Sprintf(`


import axios from "axios";

const API = "http://localhost:28080";

export async function getAll%[1]s() {
const res = await axios.get(API + "/%[2]s");
return res.data;
}

export async function create%[1]s(data) {
const res = await axios.post(API + "/%[2]s", data);
return res.data;
}
`, s.Name, strings.ToLower(s.Name))

	os.WriteFile(file, []byte(content), 0644)

}
