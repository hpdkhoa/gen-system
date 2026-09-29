package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const irSnapshotFile = ".gen/ir_snapshot.json"

// ------------------------------------------
// LoadPreviousIR
// ------------------------------------------

func LoadPreviousIR() (SchemaIR, error) { return LoadIRAt("") }

// LoadIRAt reads the IR snapshot of the project in root ("" means the working
// directory). A missing file is a first run and gives an empty IR. v26.58:
// callers that generate into a folder other than the working directory
// (engine.RunAll) read their own snapshot instead of the caller's.
func LoadIRAt(root string) (SchemaIR, error) {
	var ir SchemaIR

	data, err := os.ReadFile(filepath.Join(root, irSnapshotFile))
	if err != nil {
		// first run → no previous state
		return SchemaIR{}, nil
	}

	err = json.Unmarshal(data, &ir)
	if err != nil {
		return SchemaIR{}, err
	}

	return ir, nil
}

// ------------------------------------------
// SaveIR
// ------------------------------------------

func SaveIR(ir SchemaIR) error { return SaveIRAt("", ir) }

// SaveIRAt writes the IR snapshot of the project in root ("" means the working
// directory). v26.58.
func SaveIRAt(root string, ir SchemaIR) error {

	err := os.MkdirAll(filepath.Join(root, ".gen"), 0755)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(root, irSnapshotFile), data, 0644)
}
