// Package finetune provides tools for extracting training data from
// gen-system's repair memory and producing artifacts for LLM fine-tuning.
//
// Phase 3.3 — Private Fine-Tuning:
//
//	gen-system accumulates repair knowledge in .gen/memory.json every time
//	the healer successfully fixes a build error. The data is valuable because
//	each record pairs real code and a real error with its fix, from your own codebase.
//
//	ExportTrainingData() reads that memory and formats it as JSONL pairs
//	compatible with:
//	  • Ollama create <model> -f Modelfile   (Modelfile + JSONL)
//	  • OpenAI fine-tuning API               (chat JSONL format)
//	  • Hugging Face TRL / unsloth           (same JSONL)
//
// Usage:
//
//	gen finetune export [--min-success N] [--out path]
//	gen finetune modelfile [--base llama3] [--out path]
package finetune

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
)

// TrainingExample is a single (prompt, completion) pair for fine-tuning.
// The JSON tags match the OpenAI fine-tuning JSONL schema and are also
// compatible with Ollama's dataset format.
type TrainingExample struct {
	Messages []ChatMessage `json:"messages"`
}

// ChatMessage is one turn in a chat-format training example.
type ChatMessage struct {
	Role    string `json:"role"` // "system", "user", or "assistant"
	Content string `json:"content"`
}

// ExportStats summarises the export operation.
type ExportStats struct {
	Total       int    `json:"total_records"`
	Exported    int    `json:"exported"`
	Skipped     int    `json:"skipped_low_quality"`
	OutputPath  string `json:"output_path"`
	GeneratedAt string `json:"generated_at"`
}

// ExportTrainingData reads .gen/memory.json and writes a JSONL fine-tuning
// dataset to outputPath.
//
// minSuccess filters out records where the fix hasn't been confirmed
// successful at least N times — low-quality fixes make bad training data.
//
// The output uses OpenAI chat format (system + user + assistant messages)
// which is supported by Ollama, OpenAI, HuggingFace TRL, and unsloth.
func ExportTrainingData(outputPath string, minSuccess int) (ExportStats, error) {
	mem := intelligence.LoadMemory()

	if len(mem.Fixes) == 0 {
		return ExportStats{}, fmt.Errorf(
			"no repair records found in .gen/memory.json — " +
				"run some builds and let the healer fix errors first")
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("create output: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	stats := ExportStats{
		Total:       len(mem.Fixes),
		OutputPath:  outputPath,
		GeneratedAt: time.Now().Format(time.RFC3339),
	}

	// Sort by hash for deterministic output
	hashes := make([]string, 0, len(mem.Fixes))
	for h := range mem.Fixes {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	for _, hash := range hashes {
		rec := mem.Fixes[hash]

		// Skip low-quality fixes (failed more than succeeded, or not enough evidence)
		if rec.SuccessCount < minSuccess {
			stats.Skipped++
			continue
		}
		if rec.FailCount > rec.SuccessCount {
			stats.Skipped++
			continue
		}

		example := buildTrainingExample(rec)
		if err := enc.Encode(example); err != nil {
			fmt.Printf("  [finetune] ⚠️  encode error for hash %s: %v\n", hash, err)
			continue
		}
		stats.Exported++
	}

	return stats, nil
}

// buildTrainingExample converts a FixRecord into a chat-format training example.
// The system message establishes the code-repair task.
// The user message provides the error context.
// The assistant message is the verified fix.
func buildTrainingExample(rec intelligence.FixRecord) TrainingExample {
	system := fmt.Sprintf(
		"You are an expert Go code repair assistant specialising in %s tasks. "+
			"Given a compile error and the existing code, output ONLY the corrected "+
			"function body (no signature, no surrounding code).",
		rec.TaskType,
	)

	// v26.58 (D050): the "## Broken Code" section held summariseBody(rec.FixBody),
	// the verified fix itself, and the assistant message holds the same fix, so
	// the model learned to copy its input. The healer now records the body that
	// failed (FixRecord.BrokenBody); the section shows it, and is left out for
	// older records that have none or where it equals the fix.
	broken := ""
	if rec.BrokenBody != "" && rec.BrokenBody != rec.FixBody {
		broken = "## Broken Code\n```go\n" + summariseBody(rec.BrokenBody, 600) + "\n```\n\n"
	}
	user := fmt.Sprintf(
		"## Task\n%s\n\n## Entity\n%s\n\n## Error Type\n%s\n\n%s"+
			"Output only the corrected function body:",
		rec.TaskType,
		rec.EntityName,
		rec.ErrorType,
		broken,
	)

	assistant := rec.FixBody

	return TrainingExample{
		Messages: []ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
			{Role: "assistant", Content: assistant},
		},
	}
}

// summariseBody truncates a function body to maxLen characters,
// adding a trailing comment if truncated. This keeps training examples
// concise and avoids bloating the JSONL with 1000-line functions.
func summariseBody(body string, maxLen int) string {
	if len(body) <= maxLen {
		return body
	}
	lines := strings.Split(body[:maxLen], "\n")
	if len(lines) > 1 {
		lines = lines[:len(lines)-1] // drop partial last line
	}
	return strings.Join(lines, "\n") + "\n// ... (truncated)"
}
