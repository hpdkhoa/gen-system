package cli

import (
	"fmt"
	"os"

	"gen-system/generator/internal/finetune"
)

// Finetune is the top-level handler for `gen finetune <subcommand>`.
//
// Subcommands:
//
//	gen finetune export   [--min-success N] [--out path]
//	gen finetune modelfile [--base model]   [--out path]
//	gen finetune train    [--data path]     [--out path]
//	gen finetune stats
func Finetune() {
	args := os.Args[2:]

	if len(args) == 0 {
		printFinetuneUsage()
		return
	}

	switch args[0] {
	case "export":
		finetuneExport(args[1:])
	case "modelfile":
		finetuneModelfile(args[1:])
	case "train":
		finetuneTrainScript(args[1:])
	case "stats":
		finetuneStats()
	default:
		fmt.Printf("Unknown finetune subcommand: %s\n", args[0])
		printFinetuneUsage()
	}
}

func finetuneExport(args []string) {
	out := ".gen/training.jsonl"
	minSuccess := 1

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 < len(args) {
				i++
				out = args[i]
			}
		case "--min-success":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &minSuccess)
			}
		}
	}

	fmt.Println("📦 Exporting training data from .gen/memory.json...")
	fmt.Printf("   Output:      %s\n", out)
	fmt.Printf("   Min success: %d\n", minSuccess)
	fmt.Println("")

	stats, err := finetune.ExportTrainingData(out, minSuccess)
	if err != nil {
		fmt.Printf("❌ Export failed: %s\n", err)
		return
	}

	fmt.Printf("✅ Export complete\n")
	fmt.Printf("   Total records:  %d\n", stats.Total)
	fmt.Printf("   Exported:       %d\n", stats.Exported)
	fmt.Printf("   Skipped:        %d (low quality)\n", stats.Skipped)
	fmt.Printf("   Output file:    %s\n", stats.OutputPath)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  gen finetune modelfile        # generate Ollama Modelfile")
	fmt.Println("  gen finetune train            # generate unsloth training script")
}

func finetuneModelfile(args []string) {
	base := "codellama"
	out := "Modelfile"
	data := ".gen/training.jsonl"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base":
			if i+1 < len(args) {
				i++
				base = args[i]
			}
		case "--out":
			if i+1 < len(args) {
				i++
				out = args[i]
			}
		case "--data":
			if i+1 < len(args) {
				i++
				data = args[i]
			}
		}
	}

	cfg := finetune.DefaultModelfileConfig(data)
	cfg.BaseModel = base

	fmt.Printf("📋 Generating Modelfile (base: %s)...\n", base)

	if err := finetune.GenerateModelfile(cfg, out); err != nil {
		fmt.Printf("❌ Failed: %s\n", err)
		return
	}

	fmt.Printf("✅ Modelfile written to %s\n", out)
	fmt.Println("")
	fmt.Println("After fine-tuning:")
	fmt.Println("  ollama create gen-system-repair -f " + out)
	fmt.Println("  OLLAMA_MODEL=gen-system-repair gen server")
}

func finetuneTrainScript(args []string) {
	data := ".gen/training.jsonl"
	base := "codellama"
	out := ".gen/train.py"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--data":
			if i+1 < len(args) {
				i++
				data = args[i]
			}
		case "--base":
			if i+1 < len(args) {
				i++
				base = args[i]
			}
		case "--out":
			if i+1 < len(args) {
				i++
				out = args[i]
			}
		}
	}

	cfg := finetune.DefaultModelfileConfig(data)
	cfg.BaseModel = base

	fmt.Printf("🐍 Generating training script (base: %s)...\n", base)

	if err := finetune.GenerateTrainScript(cfg, out); err != nil {
		fmt.Printf("❌ Failed: %s\n", err)
		return
	}

	fmt.Printf("✅ Training script written to %s\n", out)
	fmt.Println("")
	fmt.Println("Run with:")
	fmt.Printf("  pip install unsloth trl datasets\n")
	fmt.Printf("  python3 %s\n", out)
}

func finetuneStats() {
	fmt.Println("📊 Fine-tuning data stats:")
	fmt.Println("")

	// Call ExportTrainingData to /dev/null just to get stats
	stats, err := finetune.ExportTrainingData(os.DevNull, 0)
	if err != nil {
		fmt.Printf("❌ %s\n", err)
		return
	}

	fmt.Printf("  Memory records:   %d\n", stats.Total)
	fmt.Printf("  Exportable:       %d\n", stats.Exported)
	fmt.Printf("  Low-quality:      %d\n", stats.Skipped)
	fmt.Println("")

	if stats.Total == 0 {
		fmt.Println("  Run some builds and let the healer fix errors to accumulate training data.")
	} else if stats.Exported < 10 {
		fmt.Println("  Tip: more training data = better model. Run more builds.")
	} else {
		fmt.Println("  ✅ Enough data to fine-tune. Run: gen finetune export")
	}
}

func printFinetuneUsage() {
	fmt.Println("")
	fmt.Println("gen finetune — private fine-tuning from repair memory")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  gen finetune export    [--min-success N] [--out path]")
	fmt.Println("    Export training pairs from .gen/memory.json → JSONL")
	fmt.Println("")
	fmt.Println("  gen finetune modelfile [--base model] [--out path]")
	fmt.Println("    Generate Ollama Modelfile for ollama create")
	fmt.Println("")
	fmt.Println("  gen finetune train     [--data path] [--base model] [--out path]")
	fmt.Println("    Generate unsloth LoRA training script (Python)")
	fmt.Println("")
	fmt.Println("  gen finetune stats")
	fmt.Println("    Show how much training data is available")
	fmt.Println("")
}
