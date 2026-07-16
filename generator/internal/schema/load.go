package schema

import (
	"encoding/json"
	"os"
)

func LoadFromFile(path string) (SchemaFile, error) {
	var s SchemaFile

	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}

	err = json.Unmarshal(data, &s)
	return s, err

}

func ParseFile(input string) SchemaFile {
	var sf SchemaFile
	json.Unmarshal([]byte(input), &sf)
	return sf
}
