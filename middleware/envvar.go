package middleware

import (
	"context"
	"log"
	"net/http"
	"os"
)

// EnvVarChecker returns a middleware that checks if the given environment variables are not empty.
// If any are empty, it responds with 500 and a message listing the missing variables.
func EnvVarChecker(envVars ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			missing := []string{}
			for _, v := range envVars {
				if os.Getenv(v) == "" {
					missing = append(missing, v)
				}
			}
			if len(missing) > 0 {
				errMsg := "Missing required environment variables: [" + joinStrings(missing, ", ") + "]"
				// Log the error so it appears in the package user's logs
				errorColor := "\033[31m" // Red
				resetColor := "\033[0m"
				log.Printf("%s[EnvVarChecker] %s%s", errorColor, errMsg, resetColor)
				// Add error to request context for upstream middleware/handlers
				type ctxKey string
				ctx := context.WithValue(r.Context(), ctxKey("envvar_error"), errMsg)
				// Pass the new context to the logger and any downstream middleware
				r2 := r.WithContext(ctx)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(errMsg))
				// Call next with the new context in case logger or others want to log error
				next.ServeHTTP(w, r2)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// joinStrings joins a slice of strings with the given separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}
