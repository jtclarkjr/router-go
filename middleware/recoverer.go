package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

const (
	// Red is the ANSI red color sequence.
	Red = "\033[31m"
	// Yellow is the ANSI yellow color sequence.
	Yellow = "\033[33m"
	// Cyan is the ANSI cyan color sequence.
	Cyan = "\033[36m"
	// Reset is the ANSI reset color sequence.
	Reset = "\033[0m"
)

// PanicHandler handles a recovered panic. Stack contains debug.Stack output.
type PanicHandler func(http.ResponseWriter, *http.Request, any, []byte)

// RecovererWithHandler returns recovery middleware whose callback owns panic
// logging and the HTTP response. A nil callback uses the existing Recoverer.
func RecovererWithHandler(handler PanicHandler) func(http.Handler) http.Handler {
	if handler == nil {
		return Recoverer
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					handler(w, r, recovered, debug.Stack())
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Recoverer is a middleware that recovers from panics, logs the panic (with a backtrace),
// and returns a 500 Internal Server Error response.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logPanic(err)

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
			fmt.Fprintf(&formattedStack, "%s  %s%s\n", Cyan, line, Reset)
		} else {
			fmt.Fprintf(&formattedStack, "%s%s\n", Yellow, line)
		}
	}

	return formattedStack.String()
}
