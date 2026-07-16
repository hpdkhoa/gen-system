package cli

import "os"

func GetSchemaPath() string {
	for i, arg := range os.Args {
		if arg == "--schema" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}
