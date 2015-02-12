package pipeline

import (
	"fmt"
	"io"
)

// InfoLogger will receive info level messages if set (use os.Stderr / os.Stdout for console output)
var InfoLogger io.Writer

// DebugLogger will receive debug level messages if set (use os.Stderr / os.Stdout for console output)
var DebugLogger io.Writer

func info(format string, a ...interface{}) (n int, err error) {
	if InfoLogger == nil {
		return 0, nil
	}
	return fmt.Fprintf(InfoLogger, "[info] "+format, a...)
}

func debug(format string, a ...interface{}) (n int, err error) {
	if DebugLogger == nil {
		return 0, nil
	}
	return fmt.Fprintf(DebugLogger, "[debug] "+format, a...)
}
