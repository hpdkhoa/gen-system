package compiler

import (
	"encoding/json"
	"os"
)

const irSnapshotFile = ".gen/ir_snapshot.json"

// ------------------------------------------
// LoadPreviousIR
// ------------------------------------------

func LoadPreviousIR() (SchemaIR, error) {
	var ir SchemaIR

	data, err := os.ReadFile(irSnapshotFile)
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

func SaveIR(ir SchemaIR) error {

	err := os.MkdirAll(".gen", 0755)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(irSnapshotFile, data, 0644)
}
