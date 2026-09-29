// D049: gen schema wrote an empty schema.generated.json and printed "Schema
// generated" when the model call failed, because GenerateSchemaFromPlan
// swallowed the error and returned "".
package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSchema_ModelFailureWritesNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // the model server does not answer
	t.Setenv("OLLAMA_HOST", srv.URL)
	if err := os.WriteFile("plan.json", []byte(`{"features":["posts"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	setArgs(t, "gen", "schema", "plan.json")
	out := captureStdout(t, Schema)
	if strings.Contains(out, "Schema generated") {
		t.Errorf("model failure reported success: %q", out)
	}
	if _, err := os.Stat("schema.generated.json"); err == nil {
		t.Errorf("model failure wrote schema.generated.json")
	}
}
