// verify_flag_v2658_test.go: v26.58.
//
// D021: an unknown --verify-understand value ("sotp") turned the Understand
// pass off without a message, so the user believed the gate ran.
package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
)

// The child process runs SetupVerifyUnderstand with the os.Args the parent
// names in GEN_TEST_VERIFY_ARG, so the test can watch it stop.
func TestVerifyUnderstand_ChildProcess(t *testing.T) {
	arg := os.Getenv("GEN_TEST_VERIFY_ARG")
	if arg == "" {
		t.Skip("helper for TestVerifyUnderstand_UnknownValueStops")
	}
	os.Args = append([]string{"gen", "continue"}, strings.Fields(arg)...)
	SetupVerifyUnderstand()
	os.Exit(0)
}

func TestVerifyUnderstand_UnknownValueStops(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestVerifyUnderstand_ChildProcess$")
	cmd.Env = append(os.Environ(), "GEN_TEST_VERIFY_ARG=--verify-understand sotp")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an unknown --verify-understand value must stop the run; it went on:\n%s", out)
	}
	if !strings.Contains(string(out), "sotp") {
		t.Errorf("the message must name the bad value:\n%s", out)
	}
}

func TestVerifyUnderstand_OffIsAccepted(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved; agent.VerifyUnderstandMode = agent.VerifyOff })
	os.Args = []string{"gen", "continue", "--verify-understand", "off"}
	SetupVerifyUnderstand()
	if agent.VerifyUnderstandMode != agent.VerifyOff {
		t.Fatalf("off gave mode %v", agent.VerifyUnderstandMode)
	}
}

// D024: "--verify-understand=stop" was not recognised; only the form with a
// space was, so the pass the user asked for stayed off.
func TestVerifyUnderstand_EqualsForm(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved; agent.VerifyUnderstandMode = agent.VerifyOff })
	for arg, want := range map[string]agent.VerifyMode{
		"--verify-understand=stop":    agent.VerifyStop,
		"--verify-understand=autofix": agent.VerifyAutofix,
	} {
		os.Args = []string{"gen", "continue", "./app", "add tags", arg}
		SetupVerifyUnderstand()
		if agent.VerifyUnderstandMode != want {
			t.Errorf("%s gave mode %v, want %v", arg, agent.VerifyUnderstandMode, want)
		}
	}
}
