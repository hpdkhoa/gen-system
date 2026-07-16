package ai

import (
	"bytes"
	"os/exec"
)

// ai.go — v23.35: the legacy CLI caller no longer hardcodes model names.
// It previously shelled out to "codellama:7b" (and "deepseek-coder:6.7b" on
// failure) with NO env override, silently contradicting the role-based model
// config the rest of the engine uses. Both now resolve through Model(), so a
// box configured for the qwen split (or any OLLAMA_MODEL) is honoured here too.
// New code should still use AskLLM() from ollama.go (HTTP API); this path is
// retained only for backward compatibility.
func Run(prompt string) string {
	cmd := exec.Command("ollama", "run", Model())
	cmd.Stdin = bytes.NewBufferString(prompt)

	out, err := cmd.Output()
	if err != nil {
		return runFallback(prompt)
	}
	return string(out)
}

func runFallback(prompt string) string {
	cmd := exec.Command("ollama", "run", Model())
	cmd.Stdin = bytes.NewBufferString(prompt)
	out, _ := cmd.Output()
	return string(out)
}
