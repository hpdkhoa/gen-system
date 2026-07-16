package engine

import (
	"strings"
	"time"
)

// ------------------------------------------
// SAFE NAME (shared)
// ------------------------------------------

func safeName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// ------------------------------------------
// TIMESTAMP (shared)
// ------------------------------------------

func timestamp() string {
	return time.Now().Format("20060102150405")
}
