// generator/internal/style/derive.go — v26.33: build a profile from what a
// team's code already does. This is the "graph as retrieval" half of the
// feature: instead of asking a model to describe the house style, the
// Understand index is measured, and every rule that comes out carries the
// count it was measured on. A rule that fewer than minAgree items follow is
// not written. The result is a starting point a human edits, marked
// source "derived:<root>" so nobody mistakes it for a decision the team made.
package style

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	minSeen    = 3    // rules need at least this many items to be stated
	agreeRatio = 0.80 // and this share must already follow the rule
	// a ban is derived only when the count is zero
)

// Derive measures root and returns a profile with evidence.
func Derive(root string) *Profile {
	return deriveSnapshot(load(root), root)
}

func deriveSnapshot(s *snapshot, root string) *Profile {
	p := &Profile{
		Version:   1,
		Name:      "derived from " + path.Base(strings.TrimRight(strings.ReplaceAll(root, "\\", "/"), "/")),
		Source:    "derived:" + root,
		DerivedAt: time.Now().UTC().Format(time.RFC3339),
		Evidence:  map[string]Evidence{},
		Gate:      Gate{Mode: ModeReport, MaxRepairs: 1},
	}
	nonTestFuncs := func() []funcInfo {
		var out []funcInfo
		for _, f := range s.funcs {
			if !f.IsTest {
				out = append(out, f)
			}
		}
		return out
	}()
	nonTestTypes := func() []typeInfo {
		var out []typeInfo
		for _, t := range s.types {
			if !t.IsTest {
				out = append(out, t)
			}
		}
		return out
	}()
	nonTestFiles := map[string]string{}
	for rel, c := range s.files {
		if !strings.HasSuffix(rel, "_test.go") {
			nonTestFiles[rel] = c
		}
	}

	// Naming: the case style most names fit, only when the majority is
	// strong. A single lowercase word fits both snake_case and camelCase, so
	// styles are scored by how many names match them rather than by naming
	// each identifier once; ties go in the listed order.
	majorityCase := func(names []string) (string, int, int) {
		best, bestN := "", 0
		for _, style := range []string{"snake_case", "camelCase", "PascalCase", "kebab-case"} {
			n := 0
			for _, name := range names {
				if matchesCase(name, style) {
					n++
				}
			}
			if n > bestN {
				best, bestN = style, n
			}
		}
		return best, bestN, len(names)
	}
	setCase := func(rule string, names []string, dst *string) {
		style, agree, seen := majorityCase(names)
		if seen >= minSeen && float64(agree) >= agreeRatio*float64(seen) {
			*dst = style
		}
		p.Evidence[rule] = Evidence{Seen: seen, Agree: agree, Note: style}
	}
	var stems, exported, local, types []string
	for _, rel := range sortedKeys(nonTestFiles) {
		stems = append(stems, fileStem(rel))
	}
	for _, f := range nonTestFuncs {
		if f.Exported {
			exported = append(exported, f.Name)
		} else {
			local = append(local, f.Name)
		}
	}
	for _, t := range nonTestTypes {
		types = append(types, t.Name)
	}
	setCase("naming.files", stems, &p.Naming.Files)
	setCase("naming.exported_funcs", exported, &p.Naming.ExportedFuncs)
	setCase("naming.local_funcs", local, &p.Naming.LocalFuncs)
	setCase("naming.types", types, &p.Naming.Types)

	// Receivers: short if most method receivers are 1 to 3 letters.
	seen, agree := 0, 0
	for _, f := range nonTestFuncs {
		if f.IsMethod && f.Receiver != "" {
			seen++
			if len(f.Receiver) <= 3 {
				agree++
			}
		}
	}
	if seen >= minSeen && float64(agree) >= agreeRatio*float64(seen) {
		p.Naming.Receivers = "short"
	}
	p.Evidence["naming.receivers"] = Evidence{Seen: seen, Agree: agree}

	// Type suffixes per layer: the trailing word most exported types share.
	byLayer := map[string][]string{}
	for _, t := range nonTestTypes {
		if t.isComponent() {
			byLayer[t.Layer] = append(byLayer[t.Layer], t.Name)
		}
	}
	for _, layer := range sortedKeys(byLayer) {
		names := byLayer[layer]
		counts := map[string]int{}
		for _, n := range names {
			if w := trailingWord(n); w != "" {
				counts[w]++
			}
		}
		best, bestN := "", 0
		for _, w := range sortedKeys(counts) {
			if counts[w] > bestN {
				best, bestN = w, counts[w]
			}
		}
		rule := "naming.type_suffixes." + layer
		unanimous := best != "" && bestN == len(names)
		if best != "" && (unanimous || len(names) >= minSeen && float64(bestN) >= agreeRatio*float64(len(names))) {
			if p.Naming.TypeSuffixes == nil {
				p.Naming.TypeSuffixes = map[string]string{}
			}
			p.Naming.TypeSuffixes[layer] = best
		}
		p.Evidence[rule] = Evidence{Seen: len(names), Agree: bestN, Note: best}
	}

	// Structure: layered when the conventional layer directories exist; the
	// directories that hold Go code become required_dirs.
	layers := map[string]bool{}
	for _, f := range nonTestFuncs {
		if f.Layer != "" {
			layers[f.Layer] = true
		}
	}
	if len(layers) >= 2 {
		p.Structure.Layout = "layered"
	}
	var dirs []string
	for _, d := range sortedKeys(s.dirs) {
		if d != "" && !strings.Contains(d, "testdata") && strings.Count(d, "/") <= 2 {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) > 0 && len(dirs) <= 12 {
		p.Structure.RequiredDirs = dirs
	}
	p.Evidence["structure.required_dirs"] = Evidence{Seen: len(s.dirs), Agree: len(dirs), Note: "directories holding Go files, depth 3 or less"}
	// File patterns: every non test file in a layer ends the same way.
	filesByLayer := map[string][]string{}
	for _, rel := range sortedKeys(nonTestFiles) {
		if l := layerOf(rel); l != "" {
			filesByLayer[l] = append(filesByLayer[l], path.Base(rel))
		}
	}
	for _, layer := range sortedKeys(filesByLayer) {
		names := filesByLayer[layer]
		suffixCounts := map[string]int{}
		for _, n := range names {
			stem := strings.TrimSuffix(n, ".go")
			if i := strings.LastIndex(stem, "_"); i >= 0 {
				suffixCounts["_"+stem[i+1:]]++
			}
		}
		best, bestN := "", 0
		for _, sfx := range sortedKeys(suffixCounts) {
			if suffixCounts[sfx] > bestN {
				best, bestN = sfx, suffixCounts[sfx]
			}
		}
		if len(names) >= 2 && best != "" && bestN == len(names) {
			if p.Structure.FilePattern == nil {
				p.Structure.FilePattern = map[string]string{}
			}
			p.Structure.FilePattern[layer] = "*" + best + ".go"
		}
		p.Evidence["structure.file_pattern."+layer] = Evidence{Seen: len(names), Agree: bestN, Note: best}
	}

	// Patterns.
	funcNames := map[string]bool{}
	for _, f := range nonTestFuncs {
		if !f.IsMethod {
			funcNames[f.Name] = true
		}
	}
	seen, agree = 0, 0
	for _, t := range nonTestTypes {
		if t.isComponent() {
			seen++
			if funcNames[constructorP+t.Name] {
				agree++
			}
		}
	}
	if seen >= 2 && float64(agree) >= agreeRatio*float64(seen) {
		p.Patterns.Require = append(p.Patterns.Require, "constructor")
	}
	p.Evidence["patterns.require.constructor"] = Evidence{Seen: seen, Agree: agree}
	if layers["repo"] {
		p.Patterns.Require = append(p.Patterns.Require, "repository")
	}
	if layers["service"] {
		p.Patterns.Require = append(p.Patterns.Require, "service-layer")
	}
	wrapSeen, wrapBad := 0, 0
	for _, f := range nonTestFuncs {
		for _, m := range errorfRe.FindAllStringSubmatch(f.Body, -1) {
			if strings.Contains(m[2], "err") {
				wrapSeen++
				if !strings.Contains(m[1], "%w") {
					wrapBad++
				}
			}
		}
	}
	if wrapSeen >= minSeen && float64(wrapSeen-wrapBad) >= agreeRatio*float64(wrapSeen) {
		p.Patterns.Require = append(p.Patterns.Require, "error-wrapping")
		p.Code.ErrorWrapping = "wrap"
	}
	p.Evidence["patterns.require.error-wrapping"] = Evidence{Seen: wrapSeen, Agree: wrapSeen - wrapBad}

	count := func(re func(string) bool) int {
		n := 0
		for _, rel := range sortedKeys(nonTestFiles) {
			if re(nonTestFiles[rel]) {
				n++
			}
		}
		return n
	}
	panics := 0
	for _, f := range nonTestFuncs {
		if panicRe.MatchString(f.Body) {
			panics++
		}
	}
	inits := count(initFuncRe.MatchString)
	reflects := count(func(c string) bool { return contains(imports(c), "reflect") })
	globals := 0
	for _, rel := range sortedKeys(nonTestFiles) {
		globals += mutableGlobals(nonTestFiles[rel])
	}
	if len(nonTestFuncs) >= minSeen {
		if panics == 0 {
			p.Patterns.Forbid = append(p.Patterns.Forbid, "panic")
		}
		if inits == 0 {
			p.Patterns.Forbid = append(p.Patterns.Forbid, "init-func")
		}
		if reflects == 0 {
			p.Patterns.Forbid = append(p.Patterns.Forbid, "reflection")
		}
		if globals == 0 {
			p.Patterns.Forbid = append(p.Patterns.Forbid, "global-state")
		}
	}
	p.Evidence["patterns.forbid.panic"] = Evidence{Seen: len(nonTestFuncs), Agree: len(nonTestFuncs) - panics}
	p.Evidence["patterns.forbid.init-func"] = Evidence{Seen: len(nonTestFiles), Agree: len(nonTestFiles) - inits}
	p.Evidence["patterns.forbid.reflection"] = Evidence{Seen: len(nonTestFiles), Agree: len(nonTestFiles) - reflects}
	p.Evidence["patterns.forbid.global-state"] = Evidence{Seen: len(nonTestFiles), Agree: len(nonTestFiles) - globals, Note: fmt.Sprintf("%d mutable globals", globals)}
	sort.Strings(p.Patterns.Require)
	sort.Strings(p.Patterns.Forbid)

	// Code shape: the largest function that exists, rounded up, so a derived
	// profile never flags the code it was derived from. Tightening is the
	// team's decision, made by editing the number.
	if len(nonTestFuncs) >= minSeen {
		var lines, branches, params []int
		for _, f := range nonTestFuncs {
			lines = append(lines, f.Lines)
			branches = append(branches, f.Branches)
			params = append(params, f.Params)
		}
		p.Code.MaxFuncLines = roundUp(percentile(lines, 1), 10)
		p.Code.MaxBranches = roundUp(percentile(branches, 1), 2)
		p.Code.MaxParams = max(percentile(params, 1), 1)
		p.Evidence["code.max_func_lines"] = Evidence{Seen: len(lines), Note: fmt.Sprintf("largest %d lines, p90 %d", percentile(lines, 1), percentile(lines, 0.90))}
		p.Evidence["code.max_branches_per_func"] = Evidence{Seen: len(branches), Note: fmt.Sprintf("largest %d branch points, p90 %d", percentile(branches, 1), percentile(branches, 0.90))}
		p.Evidence["code.max_params"] = Evidence{Seen: len(params), Note: fmt.Sprintf("largest %d params", percentile(params, 1))}
		methods := 0
		for _, f := range nonTestFuncs {
			if f.IsMethod {
				methods++
			}
		}
		if float64(methods) >= 0.5*float64(len(nonTestFuncs)) {
			p.Code.Paradigm = "struct-methods"
		} else {
			p.Code.Paradigm = "procedural"
		}
		p.Evidence["code.paradigm"] = Evidence{Seen: len(nonTestFuncs), Agree: methods, Note: "methods among functions"}
	}
	rangeN, indexN := 0, 0
	for _, f := range nonTestFuncs {
		rangeN += len(rangeLoopRe.FindAllString(f.Body, -1))
		indexN += len(indexLoopRe.FindAllString(f.Body, -1))
	}
	if rangeN+indexN >= minSeen {
		if float64(rangeN) >= agreeRatio*float64(rangeN+indexN) {
			p.Code.Iteration = "range"
		} else if float64(indexN) >= agreeRatio*float64(rangeN+indexN) {
			p.Code.Iteration = "index"
		}
	}
	p.Evidence["code.iteration"] = Evidence{Seen: rangeN + indexN, Agree: rangeN, Note: "range loops among counted loops"}

	// Imports are recorded as evidence only. Turning "what we import today"
	// into an allow list is a decision for the team, not the tool.
	impSet := map[string]bool{}
	for _, rel := range sortedKeys(nonTestFiles) {
		for _, imp := range imports(nonTestFiles[rel]) {
			impSet[imp] = true
		}
	}
	p.Evidence["code.imports_seen"] = Evidence{Seen: len(impSet), Note: strings.Join(sortedKeys(impSet), ", ")}

	p.Prompt.Exemplars = 2
	p.Prompt.ExemplarRoot = root
	return p
}

func percentile(vals []int, q float64) int {
	if len(vals) == 0 {
		return 0
	}
	c := append([]int{}, vals...)
	sort.Ints(c)
	i := int(q * float64(len(c)-1))
	return c[i]
}

func roundUp(v, step int) int {
	if step <= 0 {
		return v
	}
	if v%step == 0 {
		return max(v, step)
	}
	return (v/step + 1) * step
}
