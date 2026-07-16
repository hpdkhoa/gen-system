package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genReactPage(s schema.Entity, path string) {
	file := filepath.Join(path, "frontend/src/pages/"+s.Name+".jsx")

	stateInit := ""
	inputs := ""

	for _, f := range s.Fields {
		name := f.Name
		lower := strings.ToLower(name)

		stateInit += fmt.Sprintf("%s: \"\",\n", lower)

		inputs += fmt.Sprintf(`


<input placeholder="%s" value={form.%s} onChange={e => setForm({...form, %s: e.target.value})} />
`, name, lower, lower)
	}

	content := fmt.Sprintf(`


import React, { useEffect, useState } from "react";
import { getAll%[1]s, create%[1]s } from "../api/%[2]s";

export default function %[1]sPage() {


const [list, setList] = useState([]);
const [form, setForm] = useState({
	%[3]s
});

async function load() {
	setList(await getAll%[1]s());
}

async function submit() {
	await create%[1]s(form);
	load();
}

useEffect(() => { load(); }, []);

return (
	<div>
		<h2>%[1]s</h2>

		<div>
			%[4]s
			<button onClick={submit}>Create</button>
		</div>

		<ul>
			{list.map((i, idx) => (
				<li key={idx}>{JSON.stringify(i)}</li>
			))}
		</ul>
	</div>
);


}
`, s.Name, strings.ToLower(s.Name), stateInit, inputs)

	os.WriteFile(file, []byte(content), 0644)

}
