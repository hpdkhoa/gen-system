package engine

import (
	"fmt"
	"os"
	"time"
)

func newTraceID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func logMutation(trace, entity, fn, status, errMsg string) {

	f, _ := os.OpenFile("mutation.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()

	line := fmt.Sprintf(
		"%s | %s | %s | %s | %s | %s\n",
		time.Now().Format(time.RFC3339),
		trace,
		entity,
		fn,
		status,
		errMsg,
	)

	f.WriteString(line)
}
