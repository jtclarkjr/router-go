package middleware

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
)

// EnvVarChecker returns middleware that reports missing environment variables.
// The current behavior writes a 500 response and then calls the next handler.
func EnvVarChecker(envVars ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			missing := make([]string, 0, len(envVars))
			for _, name := range envVars {
				if os.Getenv(name) == "" {
					missing = append(missing, name)
				}
			}
			if len(missing) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			errMsg := "Missing required environment variables: [" + strings.Join(missing, ", ") + "]"
			log.Printf("%s[EnvVarChecker] %s%s", Red, errMsg, Reset)

			type ctxKey string
			ctx := context.WithValue(r.Context(), ctxKey("envvar_error"), errMsg)
			requestWithError := r.WithContext(ctx)

			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(errMsg)); err != nil {
				log.Printf("Failed to write error response: %v", err)
			}
			next.ServeHTTP(w, requestWithError)
		})
	}
}
