package mobile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// GenerateScreen writes mobile/<entity>_screen.js, a React Native form and
// list for one entity. It returns an error when the file cannot be written.
func GenerateScreen(e schema.Entity, path string) error {

	name := e.Name
	lower := strings.ToLower(name)

	content := fmt.Sprintf(`


import { View, Text, TextInput, Button } from "react-native";
import { useState, useEffect } from "react";

export default function %sScreen() {

const [form, setForm] = useState({});
const [list, setList] = useState([]);

useEffect(() => {
fetch("http://10.0.2.2:28080/%ss")
.then(r => r.json())
.then(setList);
}, []);

return ( <View>


  <Text>%s Form</Text>


`, name, lower, name)

	for _, f := range e.Fields {
		content += fmt.Sprintf(`
  <TextInput
    placeholder="%s"
    onChangeText={v => setForm({...form, %s: v})}
  />


`, f.Name, f.Name)
	}

	content += fmt.Sprintf(`
  <Button title="Create" onPress={() => {
    fetch("http://10.0.2.2:28080/%ss", {
      method: "POST",
      body: JSON.stringify(form)
    });
  }} />

  <Text>List</Text>

  {list.map(i => (
    <Text key={i.id}>{JSON.stringify(i)}</Text>
  ))}

</View>


);
}
`, lower)

	// v26.57 (D003): create mobile/ first and report a failed write. In an agent
	// build into a fresh folder nothing created mobile/ before the first
	// generate_mobile task, so the old bare os.WriteFile failed without a
	// message and App.js imported a screen that did not exist.
	dir := filepath.Join(path, "mobile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mobile screen for %s: %w", name, err)
	}
	file := filepath.Join(dir, lower+"_screen.js")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return fmt.Errorf("mobile screen for %s: %w", name, err)
	}
	return nil
}
