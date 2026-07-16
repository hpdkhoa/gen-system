// frontend_test.go — v19: COBOL is now registered in the default build.
// Updated test entries: Language("cobol") expects true, "file.cobol"
// returns a CobolFrontend, ParseFile("file.cobol", ...) returns a module.
package ast

import (
	"testing"
)

// The real frontends register themselves via init() when the test
// binary is built, so the registry is pre-populated.  These tests
// verify the registry contract without adding duplicate entries.

func TestRegistry_ForLanguage(t *testing.T) {
	cases := []struct {
		lang Language
		want bool // should be registered
	}{
		{LangGo, true},
		{LangTS, true},
		{LangPython, true},
		{LangCAGen, true},
		{LangCOBOL, true}, // v19: ungated
		{LangUnknown, false},
	}
	for _, c := range cases {
		fe := ForLanguage(c.lang)
		got := fe != nil
		if got != c.want {
			t.Errorf("ForLanguage(%q): got nil=%v, want nil=%v", c.lang, !got, !c.want)
		}
		if fe != nil && fe.Language() != c.lang {
			t.Errorf("frontend for %q reports Language() = %q", c.lang, fe.Language())
		}
	}
}

func TestRegistry_ForFile(t *testing.T) {
	cases := []struct {
		path     string
		wantLang Language
	}{
		{"main.go", LangGo},
		{"handler.go", LangGo},
		{"app.ts", LangTS},
		{"component.tsx", LangTS},
		{"script.py", LangPython},
		{"model.pyw", LangPython},
		{"order.ad", LangCAGen},
		{"workflow.cagen", LangCAGen},
		{"logic.gel", LangCAGen},
		{"program.cob", LangCOBOL},   // v19
		{"main.cbl", LangCOBOL},      // v19
		{"book.cpy", LangCOBOL},      // v19
		{"app.cobol", LangCOBOL},     // v19
		{"readme.md", LangUnknown},
		{"data.json", LangUnknown},
		{"", LangUnknown},
	}
	for _, c := range cases {
		fe := ForFile(c.path)
		if c.wantLang == LangUnknown {
			if fe != nil {
				t.Errorf("ForFile(%q) should be nil, got %v", c.path, fe.Language())
			}
		} else {
			if fe == nil {
				t.Errorf("ForFile(%q) = nil, want language %q", c.path, c.wantLang)
				continue
			}
			if fe.Language() != c.wantLang {
				t.Errorf("ForFile(%q).Language() = %q, want %q", c.path, fe.Language(), c.wantLang)
			}
		}
	}
}

func TestRegistry_DetectLanguage(t *testing.T) {
	cases := []struct{ path, want string }{
		{"foo.go", "go"},
		{"foo.ts", "typescript"},
		{"foo.tsx", "typescript"},
		{"foo.py", "python"},
		{"foo.ad", "cagen"},
		{"foo.cob", "cobol"}, // v19
		{"foo.cbl", "cobol"}, // v19
		{"foo.md", "unknown"},
	}
	for _, c := range cases {
		got := string(DetectLanguage(c.path))
		if got != c.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestRegistry_AllFrontends(t *testing.T) {
	all := All()
	langs := map[Language]bool{}
	for _, fe := range all {
		l := fe.Language()
		if langs[l] {
			t.Errorf("duplicate frontend for language %q", l)
		}
		langs[l] = true
		if len(fe.Extensions()) == 0 {
			t.Errorf("frontend %q has no extensions", l)
		}
	}
	required := []Language{LangGo, LangTS, LangPython, LangCAGen, LangCOBOL}
	for _, req := range required {
		if !langs[req] {
			t.Errorf("missing required frontend for language %q", req)
		}
	}
}

func TestRegistry_ParseFile_UnknownExtension(t *testing.T) {
	// v19: .cobol is now a recognised extension; use .xyz instead.
	m, err := ParseFile("file.xyz", []byte("some content"))
	if err != nil {
		t.Errorf("ParseFile(unknown ext) returned error: %v", err)
	}
	if m != nil {
		t.Errorf("ParseFile(unknown ext) should return nil module, got %+v", m)
	}
}

func TestRegistry_ParseFile_EmptySource(t *testing.T) {
	m, err := ParseFile("empty.go", []byte{})
	if err != nil {
		t.Errorf("ParseFile(empty go) returned error: %v", err)
	}
	if m == nil {
		t.Error("ParseFile(empty go) returned nil module")
	}
}
