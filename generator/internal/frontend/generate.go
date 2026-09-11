package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func GenerateForm(e schema.Entity, path string) {

	var inputs []string

	for _, f := range e.Fields {
		inputs = append(inputs,
			fmt.Sprintf(`


<input
placeholder="%s"
value={form.%s || ""}
onChange={e => setForm({...form, %s: e.target.value})}
/>
`, f.Name, f.Name, f.Name))
	}

	// relation dropdown
	for _, r := range e.Relations {
		if r.Type == "belongsTo" {
			inputs = append(inputs,
				fmt.Sprintf(`


<select onChange={e => setForm({...form, %s: e.target.value})}>
{options.%s?.map(o => ( <option value={o.id}>{o.id}</option>
))} </select>
`, r.ForeignKey, r.Target))
		}
	}

	content := fmt.Sprintf(`


import { useState, useEffect } from "react";

export default function %[1]sForm() {

const [form, setForm] = useState({});
const [options, setOptions] = useState({});

useEffect(() => {
`, e.Name)

	for _, r := range e.Relations {
		content += fmt.Sprintf(`
fetch("/%s").then(r=>r.json()).then(data => {
  setOptions(o => ({...o, %s: data}))
})


`, strings.ToLower(r.Target)+"s", r.Target)
	}

	content += `


}, []);

return ( <div>
`

	content += strings.Join(inputs, "\n")

	content += fmt.Sprintf(`
  <button onClick={() => {
    fetch("/%s", {
      method: "POST",
      body: JSON.stringify(form)
    })
  }}>
    Create
  </button>
</div>


);
}
`, strings.ToLower(e.Name)+"s")

	file := filepath.Join(path, "frontend/"+strings.ToLower(e.Name)+"Form.jsx")
	os.WriteFile(file, []byte(content), 0644)

}

func GenerateList(e schema.Entity, path string) {

	content := fmt.Sprintf(`


import { useEffect, useState } from "react";

export default function %[1]sList() {

const [data, setData] = useState([]);

useEffect(() => {
fetch("/%s")
.then(r => r.json())
.then(setData);
}, []);

return ( <div>
{data.map(item => ( <div key={item.id}>
`, e.Name, strings.ToLower(e.Name)+"s")

	for _, f := range e.Fields {
		content += fmt.Sprintf(`<p>%s: {item.%s}</p>`, f.Name, f.Name)
	}

	for _, r := range e.Relations {
		content += fmt.Sprintf(`<p>%s: {item.%s?.id}</p>`, r.Target, r.Target)
	}

	content += `
    </div>
  ))}
</div>


);
}
`

	file := filepath.Join(path, "frontend/"+strings.ToLower(e.Name)+"List.jsx")
	os.WriteFile(file, []byte(content), 0644)

}
