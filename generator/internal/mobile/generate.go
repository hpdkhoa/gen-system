package mobile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func GenerateScreen(e schema.Entity, path string) {

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

	file := filepath.Join(path, "mobile/"+lower+"_screen.js")
	os.WriteFile(file, []byte(content), 0644)

}
