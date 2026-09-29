package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// writeFrontendFile writes one file into <root>/frontend, creating the folder
// first.
//
// v26.58 (D032): GenerateForm and GenerateList called a bare os.WriteFile and
// dropped its result. On an output folder with no frontend folder yet the
// write failed, nothing was reported, and the generate_frontend task counted
// as done. Creating the folder and returning the error fixes both.
func writeFrontendFile(root, name, content string) error {
	dir := filepath.Join(root, "frontend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("frontend %s: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		return fmt.Errorf("frontend %s: %w", name, err)
	}
	return nil
}

// jsProp reads the JSON key of an object in JavaScript: obj.key, or
// obj['key'] when the key is not a valid identifier (a JSON key can start with
// a digit).
func jsProp(obj, key string) string {
	if p := tsPropName(key); p != key {
		return obj + "[" + p + "]"
	}
	return obj + "." + key
}

// GenerateForm writes frontend/<lower>Form.jsx. It returns an error when the
// file cannot be written.
func GenerateForm(e schema.Entity, path string) error {

	var inputs []string

	// v26.58 (D035): the form sends the JSON keys the generated model reads
	// (schema.JSONKey), not the raw field names. "due date" gave the invalid
	// form.due date, and "createdAt" a key the model never reads (createdat).
	for _, f := range e.Fields {
		key := schema.JSONKey(f.Name)
		inputs = append(inputs,
			fmt.Sprintf(`


<input
placeholder="%s"
value={%s || ""}
onChange={e => setForm({...form, %s: e.target.value})}
/>
`, f.Name, jsProp("form", key), tsPropName(key)))
	}

	// relation dropdown
	for _, r := range e.Relations {
		if r.Type == "belongsTo" {
			// v26.58 (D035): the foreign key under the name the model gives it
			// (schema.RelationFields): "OrderItemID" is sent as order_item_id.
			fk := r.ForeignKey
			if strings.TrimSpace(fk) == "" {
				fk = r.Target + "ID"
			}
			inputs = append(inputs,
				fmt.Sprintf(`


<select onChange={e => setForm({...form, %s: e.target.value})}>
{options.%s?.map(o => ( <option value={o.id}>{o.id}</option>
))} </select>
`, tsPropName(schema.JSONKey(schema.FKFieldName(fk))), r.Target))
		}
	}

	content := fmt.Sprintf(`


import { useState, useEffect } from "react";

export default function %[1]sForm() {

const [form, setForm] = useState({});
const [options, setOptions] = useState({});

useEffect(() => {
`, e.Name)

	// v26.58 (D035): request paths come from schema.CollectionPath, the rule
	// the generated backend registers its routes with. strings.ToLower(name) +
	// "s" gave /addresss and /order_items where the backend serves /address
	// and /orderitems.
	for _, r := range e.Relations {
		content += fmt.Sprintf(`
fetch("%s").then(r=>r.json()).then(data => {
  setOptions(o => ({...o, %s: data}))
})


`, schema.CollectionPath(r.Target), r.Target)
	}

	content += `


}, []);

return ( <div>
`

	content += strings.Join(inputs, "\n")

	content += fmt.Sprintf(`
  <button onClick={() => {
    fetch("%s", {
      method: "POST",
      body: JSON.stringify(form)
    })
  }}>
    Create
  </button>
</div>


);
}
`, schema.CollectionPath(e.Name)) // v26.58 (D035): the backend route

	// v26.58 (D032): create frontend/ and return the write error.
	return writeFrontendFile(path, strings.ToLower(e.Name)+"Form.jsx", content)
}

// GenerateList writes frontend/<lower>List.jsx. It returns an error when the
// file cannot be written.
func GenerateList(e schema.Entity, path string) error {

	content := fmt.Sprintf(`


import { useEffect, useState } from "react";

export default function %[1]sList() {

const [data, setData] = useState([]);

useEffect(() => {
fetch("%s")
.then(r => r.json())
.then(setData);
}, []);

return ( <div>
{data.map(item => ( <div key={item.id}>
`, e.Name, schema.CollectionPath(e.Name)) // v26.58 (D035): the backend route

	// v26.58 (D035): read the JSON key the model writes, not the field name.
	for _, f := range e.Fields {
		content += fmt.Sprintf(`<p>%s: {%s}</p>`, f.Name, jsProp("item", schema.JSONKey(f.Name)))
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

	// v26.58 (D032): create frontend/ and return the write error.
	return writeFrontendFile(path, strings.ToLower(e.Name)+"List.jsx", content)
}
