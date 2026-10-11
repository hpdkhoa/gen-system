package finetune

import (
	"fmt"
	"os"
	"time"
)

// ModelfileConfig controls the generated Ollama Modelfile.
type ModelfileConfig struct {
	BaseModel   string // e.g. "llama3", "codellama", "mistral"
	Name        string // your custom model name, e.g. "gen-system-repair"
	DatasetPath string // path to the JSONL dataset (from ExportTrainingData)
	Temperature float64
	ContextSize int // num_ctx; larger = more context for repair prompts
}

// DefaultModelfileConfig returns sensible defaults for a Go code repair model.
// codellama:7b is a good base: already trained on code, fast on CPU.
func DefaultModelfileConfig(datasetPath string) ModelfileConfig {
	return ModelfileConfig{
		BaseModel:   "codellama",
		Name:        "gen-system-repair",
		DatasetPath: datasetPath,
		Temperature: 0.1, // low temp for deterministic repairs
		ContextSize: 8192,
	}
}

// GenerateModelfile writes an Ollama Modelfile to outputPath.
//
// The Modelfile includes:
//   - FROM directive (base model)
//   - PARAMETER tuning (temperature, context size)
//   - SYSTEM prompt that matches the fine-tuning system message
//   - Instructions for training with `ollama create`
//
// Fine-tuning with Ollama (LoRA):
//
//	ollama create gen-system-repair -f Modelfile
//
// Note: Ollama's `ollama create` command supports fine-tuning via
// the ADAPTER directive when using GGUF LoRA adapters. For full
// fine-tuning workflows (generating the LoRA adapter itself), use
// the exported JSONL with unsloth or axolotl, then convert to GGUF.
func GenerateModelfile(cfg ModelfileConfig, outputPath string) error {
	// We use concatenation for the triple backticks because Go raw strings
	// cannot contain a backtick character.
	tripleBacktick := "`" + "`" + "`"

	content := fmt.Sprintf(`# gen-system Repair Model — Ollama Modelfile
# Generated: %s
#
# Usage:
#   1. Export training data:   gen finetune export --out training.jsonl
#   2. Fine-tune (unsloth):    python3 train.py --data training.jsonl --base %s
#   3. Convert to GGUF:        python3 convert_to_gguf.py adapter/ --out model.gguf
#   4. Create Ollama model:    ollama create %s -f Modelfile
#   5. Use in gen-system:      OLLAMA_MODEL=%s gen server
#
# For direct (no fine-tune) use with system prompt only:
#   ollama create %s -f Modelfile

FROM %s

PARAMETER temperature %.2f
PARAMETER num_ctx %d
PARAMETER num_predict 2048
PARAMETER stop "<|eot_id|>"
PARAMETER stop "%s"

SYSTEM """
You are gen-system's Go code repair specialist.

Your role: given a compile error and broken Go function body, produce a
corrected function body that fixes the error while preserving the function's
original intent.

Rules:
- Output ONLY the function body (the code between { and }, not the signature)
- No markdown, no explanations, no backticks — raw Go code only
- Preserve all existing business logic; fix only the compilation error
- Keep the same return types and variable names where possible
- If the error is a missing import, add the import usage correctly

Error categories you handle:
  undefined:    missing variable, function, or type reference
  type_mismatch: incompatible types in assignment or function call
  missing_return: not all code paths return a value
  syntax_error: malformed Go syntax
  import_cycle:  circular package dependency
  nil_deref:    potential nil pointer dereference
"""

# ADAPTER (uncomment after fine-tuning):
# ADAPTER ./gen-system-repair.gguf
`,
		time.Now().Format("2006-01-02 15:04:05"),
		cfg.BaseModel, cfg.Name, cfg.Name,
		// v18.3: the args list was off by one. The "direct (no fine-tune)"
		// ollama-create line needs cfg.Name (the model name to create), and
		// only after that comes FROM cfg.BaseModel and PARAMETER temperature
		// cfg.Temperature. Without cfg.Name here, cfg.BaseModel was binding
		// to the wrong %s and cfg.Temperature (float64) was being formatted
		// with %s, which is a vet/build error.
		cfg.Name, cfg.BaseModel, cfg.Temperature, cfg.ContextSize,
		tripleBacktick,
	)

	return os.WriteFile(outputPath, []byte(content), 0644)
}

// GenerateTrainScript writes a minimal unsloth/LoRA training script.
// This is a starting point. Users need to install unsloth and
// adjust hyperparameters for their hardware.
func GenerateTrainScript(cfg ModelfileConfig, outputPath string) error {
	// Note: Python's triple quotes """ are fine inside Go backticks.
	// Only literal backticks cause issues.
	content := fmt.Sprintf(`#!/usr/bin/env python3
"""
gen-system LoRA fine-tuning script (unsloth)
Generated: %s

Requirements:
  pip install unsloth transformers datasets trl

Usage:
  python3 train.py --data %s --base %s

The trained adapter will be saved to ./adapter/
Then convert to GGUF:
  python3 -m unsloth.convert_to_gguf adapter/ --quantize q4_k_m --output gen-system-repair.gguf
"""

import argparse, json
from datasets import Dataset
from unsloth import FastLanguageModel
from trl import SFTTrainer
from transformers import TrainingArguments

def load_jsonl(path):
    examples = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                examples.append(json.loads(line))
    return examples

def format_prompt(example):
    msgs = example["messages"]
    text = ""
    for m in msgs:
        if m["role"] == "system":
            text += f"<|system|>\n{m['content']}\n"
        elif m["role"] == "user":
            text += f"<|user|>\n{m['content']}\n"
        elif m["role"] == "assistant":
            text += f"<|assistant|>\n{m['content']}<|eot_id|>\n"
    return {"text": text}

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--data",    default="%s")
    parser.add_argument("--base",    default="%s")
    parser.add_argument("--epochs",  type=int, default=3)
    parser.add_argument("--output",  default="./adapter")
    args = parser.parse_args()

    print(f"Loading base model: {args.base}")
    model, tokenizer = FastLanguageModel.from_pretrained(
        model_name=args.base,
        max_seq_length=8192,
        load_in_4bit=True,
    )
    model = FastLanguageModel.get_peft_model(
        model,
        r=16,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj"],
        lora_alpha=16,
        lora_dropout=0,
        bias="none",
    )

    print(f"Loading dataset: {args.data}")
    raw = load_jsonl(args.data)
    formatted = [format_prompt(ex) for ex in raw]
    dataset = Dataset.from_list(formatted)
    print(f"  {len(dataset)} training examples")

    trainer = SFTTrainer(
        model=model,
        train_dataset=dataset,
        dataset_text_field="text",
        max_seq_length=8192,
        tokenizer=tokenizer,
        args=TrainingArguments(
            per_device_train_batch_size=2,
            gradient_accumulation_steps=4,
            num_train_epochs=args.epochs,
            learning_rate=2e-4,
            fp16=True,
            output_dir=args.output,
            save_steps=50,
            logging_steps=10,
        ),
    )

    print("Training...")
    trainer.train()
    model.save_pretrained(args.output)
    tokenizer.save_pretrained(args.output)
    print(f"Adapter saved to {args.output}")
    print("")
    print("Next: convert to GGUF with:")
    print(f"  python3 -m unsloth.convert_to_gguf {args.output} --quantize q4_k_m --output gen-system-repair.gguf")
    print("Then: ollama create %s -f Modelfile")

if __name__ == "__main__":
    main()
`,
		time.Now().Format("2006-01-02"),
		cfg.DatasetPath, cfg.BaseModel,
		cfg.DatasetPath, cfg.BaseModel, cfg.Name,
	)

	// return os.WriteFile(outputPath, []byte(content), 0644)
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write train script: %w", err)
	}
	return nil
}
