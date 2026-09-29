package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

const (
	markerStart = "{/* gen-system:start %s */}"
	markerEnd   = "{/* gen-system:end %s */}"
)

// EvolveForm safely updates an existing Form component when the schema changes.
//
// Strategy:
//  1. If the file has gen-system markers → replace only the marker block
//  2. If no markers found → treat as first run, full overwrite with markers
//
// Custom code outside the marker block is always preserved.
func EvolveForm(e schema.Entity, rootPath string) {
	formPath := filepath.Join(rootPath, "frontend", "src", "components", e.Name+"Form.jsx")

	existing, err := os.ReadFile(formPath)
	if err != nil {
		// v26.58 (D031): the fallback called GenerateForm, which writes
		// frontend/<lower>Form.jsx, a path this function never reads, so no run
		// ever created the marker file. Write the file we read, with markers.
		generateFormWithMarkers(e, rootPath)
		fmt.Printf("[evolve] ✅ Form generated with evolution markers: %s\n", formPath)
		return
	}

	content := string(existing)
	blockName := "FieldInputs"
	start := fmt.Sprintf(markerStart, blockName)
	end := fmt.Sprintf(markerEnd, blockName)

	// Build new field inputs block
	newBlock := buildFormInputs(e)

	if strings.Contains(content, start) && strings.Contains(content, end) {
		// Markers found — replace only the inner block
		startIdx := strings.Index(content, start) + len(start)
		endIdx := strings.Index(content, end)
		if startIdx > 0 && endIdx > startIdx {
			updated := content[:startIdx] + "\n" + newBlock + "\n      " + content[endIdx:]
			if err := os.WriteFile(formPath, []byte(updated), 0644); err != nil {
				fmt.Printf("[evolve] ❌ write failed for %s: %v\n", formPath, err)
				return
			}
			fmt.Printf("[evolve] ✅ Form updated safely (custom code preserved): %s\n", formPath)
			return
		}
	}

	// No markers — safe first run: regenerate with markers embedded
	generateFormWithMarkers(e, rootPath)
	fmt.Printf("[evolve] ✅ Form generated with evolution markers: %s\n", formPath)
}

// EvolveList safely updates an existing List component when the schema changes.
func EvolveList(e schema.Entity, rootPath string) {
	listPath := filepath.Join(rootPath, "frontend", "src", "components", e.Name+"List.jsx")

	existing, err := os.ReadFile(listPath)
	if err != nil {
		// v26.58 (D031): write the path this function reads, with markers, not
		// GenerateList's frontend/<lower>List.jsx, so the next run can evolve it.
		generateListWithMarkers(e, rootPath)
		fmt.Printf("[evolve] ✅ List generated with evolution markers: %s\n", listPath)
		return
	}

	content := string(existing)
	blockName := "TableColumns"
	start := fmt.Sprintf(markerStart, blockName)
	end := fmt.Sprintf(markerEnd, blockName)

	newBlock := buildListColumns(e)

	if strings.Contains(content, start) && strings.Contains(content, end) {
		startIdx := strings.Index(content, start) + len(start)
		endIdx := strings.Index(content, end)
		if startIdx > 0 && endIdx > startIdx {
			updated := content[:startIdx] + "\n" + newBlock + "\n          " + content[endIdx:]
			if err := os.WriteFile(listPath, []byte(updated), 0644); err != nil {
				fmt.Printf("[evolve] ❌ write failed for %s: %v\n", listPath, err)
				return
			}
			fmt.Printf("[evolve] ✅ List updated safely (custom code preserved): %s\n", listPath)
			return
		}
	}

	generateListWithMarkers(e, rootPath)
	fmt.Printf("[evolve] ✅ List generated with evolution markers: %s\n", listPath)
}

// ─────────────────────────────────────────────
// builders
// ─────────────────────────────────────────────

func buildFormInputs(e schema.Entity) string {
	var sb strings.Builder
	for _, f := range e.Fields {
		if strings.EqualFold(f.Name, "id") {
			continue // id is not user-editable
		}
		// v26.58 (D035): use the JSON key the generated model reads
		// (schema.JSONKey), not the raw field name.
		key := schema.JSONKey(f.Name)
		sb.WriteString(fmt.Sprintf(
			"        <div className=\"form-field\">\n"+
				"          <label>%s</label>\n"+
				"          <input\n"+
				"            placeholder=\"%s\"\n"+
				"            value={%s || \"\"}\n"+
				"            onChange={e => setForm({...form, %s: e.target.value})}\n"+
				"          />\n"+
				"        </div>\n",
			f.Name, f.Name, jsProp("form", key), tsPropName(key),
		))
	}
	return sb.String()
}

func buildListColumns(e schema.Entity) string {
	var sb strings.Builder
	for _, f := range e.Fields {
		// v26.58 (D035): read the JSON key the model writes.
		sb.WriteString(fmt.Sprintf(
			"              <td>{%s}</td>\n", jsProp("item", schema.JSONKey(f.Name))))
	}
	return sb.String()
}

func generateFormWithMarkers(e schema.Entity, rootPath string) {
	inputs := buildFormInputs(e)
	startMarker := fmt.Sprintf(markerStart, "FieldInputs")
	endMarker := fmt.Sprintf(markerEnd, "FieldInputs")

	content := fmt.Sprintf(`import React, { useState } from 'react';

export default function %sForm({ onSubmit }) {
  const [form, setForm] = useState({});

  return (
    <form onSubmit={e => { e.preventDefault(); onSubmit(form); }}>
      %s
      %s
      %s
      <button type="submit">Submit</button>
    </form>
  );
}
`, e.Name, startMarker, inputs, endMarker)

	dir := filepath.Join(rootPath, "frontend", "src", "components")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, e.Name+"Form.jsx"), []byte(content), 0644)
}

func generateListWithMarkers(e schema.Entity, rootPath string) {
	columns := buildListColumns(e)
	headers := ""
	for _, f := range e.Fields {
		headers += fmt.Sprintf("              <th>%s</th>\n", f.Name)
	}
	startMarker := fmt.Sprintf(markerStart, "TableColumns")
	endMarker := fmt.Sprintf(markerEnd, "TableColumns")

	content := fmt.Sprintf(`import React from 'react';

export default function %sList({ items = [] }) {
  return (
    <table>
      <thead>
        <tr>
%s        </tr>
      </thead>
      <tbody>
        {items.map((item, i) => (
          <tr key={item.id || i}>
          %s
          %s
          %s
          </tr>
        ))}
      </tbody>
    </table>
  );
}
`, e.Name, headers, startMarker, columns, endMarker)

	dir := filepath.Join(rootPath, "frontend", "src", "components")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, e.Name+"List.jsx"), []byte(content), 0644)
}
