package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

// ANSI color codes
const (
	Red    = "\033[31m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Reset  = "\033[0m"
)

// Recoverer is a middleware that recovers from panics, logs the panic (with a backtrace),
// and returns a 500 Internal Server Error response.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the panic details
				logPanic(err)

				// Respond with 500 Internal Server Error
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logPanic logs the panic details and stack trace to stderr with colored output.
func logPanic(err any) {
	stack := debug.Stack()
	fmt.Fprintf(os.Stderr, "%sPANIC: %v%s\n", Red, err, Reset)
	fmt.Fprintf(os.Stderr, "%sSTACK TRACE:%s\n%s\n", Yellow, Reset, formatStack(stack))
}

// formatStack formats the stack trace for better readability with colored output.
func formatStack(stack []byte) string {
	lines := strings.Split(string(stack), "\n")
	var formattedStack bytes.Buffer

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ".go:") {
			formattedStack.WriteString(fmt.Sprintf("%s  %s%s\n", Cyan, line, Reset))
		} else {
			formattedStack.WriteString(fmt.Sprintf("%s%s\n", Yellow, line))
		}
	}

	return formattedStack.String()
}
