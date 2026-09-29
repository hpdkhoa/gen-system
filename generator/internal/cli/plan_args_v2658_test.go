// D025: Plan appended every argument except --v2 to the goal, so
// `gen plan --out ./x "a blog"` sent "--out ./x a blog" to the planner and
// stored it as the goal of the plan file.
package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlan_OutFlagNotInGoal(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // no model: Plan prints the goal, then the model call fails
	t.Setenv("OLLAMA_HOST", srv.URL)
	setArgs(t, "gen", "plan", "--out", "./x", "a blog", "--env=dev", "--force")
	out := captureStdout(t, Plan)
	if !strings.Contains(out, "Goal: a blog\n") {
		t.Errorf("want the goal \"a blog\", got %q", out)
	}
}
