// generator/internal/style/opcheck.go (v26.33): the operation level check.
//
// The compile gate in engine/service_logic.go sees one model written method
// body at a time, before it becomes a file. This is the same moment for
// style: a body that compiles but breaks a team rule is sent back to the
// model with the rule named, the same way a build error is, and under the
// same repair cap. Style never stubs an operation; a body that compiles but
// is off style stays, and the gate reports it. Correctness has its own gate,
// and only that gate can replace a body with a stub.
package style

import (
	"fmt"
	"strings"
)

// CheckOperation returns the findings for one operation body (plus its
// helper functions). Rules that need the whole tree (directories, file
// names, constructors) are not applied here; the tree gate runs later.
func CheckOperation(p *Profile, name, body string, helpers []string) []Finding {
	if p == nil {
		return nil
	}
	text := body
	if len(helpers) > 0 {
		text += "\n" + strings.Join(helpers, "\n")
	}
	var out []Finding
	add := func(rule, detail string) {
		out = append(out, Finding{Rule: rule, Severity: p.SeverityOf(rule), Symbol: name, Detail: detail, Fixable: true})
	}
	for _, rule := range p.ActiveRules() {
		switch rule {
		case "code.max_func_lines":
			lines := strings.Count(strings.TrimSpace(body), "\n") + 3 // signature and closing brace
			if lines > p.Code.MaxFuncLines {
				add(rule, fmt.Sprintf("%s is %d lines (max %d)", name, lines, p.Code.MaxFuncLines))
			}
		case "code.max_branches_per_func":
			if n := branchesFromText(body); n > p.Code.MaxBranches {
				add(rule, fmt.Sprintf("%s has %d branch points (max %d)", name, n, p.Code.MaxBranches))
			}
		case "patterns.forbid.panic":
			if panicRe.MatchString(text) {
				add(rule, name+" calls panic")
			}
		case "patterns.forbid.reflection":
			if strings.Contains(text, "reflect.") {
				add(rule, name+" uses reflect")
			}
		case "patterns.require.error-wrapping", "code.error_wrapping":
			if rule == "code.error_wrapping" && contains(p.Patterns.Require, "error-wrapping") {
				continue
			}
			if n := errorfWithoutWrap(text); n > 0 {
				add(rule, fmt.Sprintf("%d fmt.Errorf call(s) in %s pass err without %%w", n, name))
			}
		case "code.iteration":
			if p.Code.Iteration == "range" && indexLoopRe.MatchString(text) {
				add(rule, name+" uses an index loop over len(); the team style is range")
			}
			if p.Code.Iteration == "index" && rangeLoopRe.MatchString(text) {
				add(rule, name+" uses range; the team style is an index loop")
			}
		case "code.forbidden_imports":
			for _, imp := range p.Code.ForbiddenImports {
				pkg := imp
				if i := strings.LastIndex(imp, "/"); i >= 0 {
					pkg = imp[i+1:]
				}
				if strings.Contains(text, pkg+".") {
					add(rule, fmt.Sprintf("%s uses package %s, which is forbidden", name, imp))
				}
			}
		case "naming.local_funcs":
			for _, h := range helpers {
				if hn := helperName(h); hn != "" && !matchesCase(hn, p.Naming.LocalFuncs) && !matchesCase(hn, "PascalCase") {
					add(rule, fmt.Sprintf("helper %s is not %s", hn, p.Naming.LocalFuncs))
				}
			}
		}
	}
	return out
}

func helperName(src string) string {
	t := strings.TrimSpace(src)
	if !strings.HasPrefix(t, "func ") {
		return ""
	}
	t = strings.TrimPrefix(t, "func ")
	if i := strings.IndexAny(t, "(["); i > 0 {
		return strings.TrimSpace(t[:i])
	}
	return ""
}

// RenderFindings formats findings as the text sent back to the model, or
// printed for a human. Empty when there are none.
func RenderFindings(fs []Finding) string {
	if len(fs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", f.Severity, f.Rule, f.Detail)
	}
	return b.String()
}

// HasError reports whether any finding is error severity.
func HasError(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == SevError {
			return true
		}
	}
	return false
}
