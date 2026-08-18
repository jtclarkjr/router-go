package middleware

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
)

// MissingEnvVarsError reports environment variables that were empty when
// startup validation ran.
type MissingEnvVarsError struct {
	Names []string
}

func (e *MissingEnvVarsError) Error() string {
	return "Missing required environment variables: [" + strings.Join(e.Names, ", ") + "]"
}

// MissingEnvVars returns required environment variable names whose values are
// empty. Names remain in caller-provided order.
func MissingEnvVars(envVars ...string) []string {
	missing := make([]string, 0, len(envVars))
	for _, name := range envVars {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// RequireEnvVars validates required environment variables during startup.
// It returns nil when every value is nonempty and does not terminate the
// process, allowing applications to choose their own logging and exit policy.
func RequireEnvVars(envVars ...string) error {
	missing := MissingEnvVars(envVars...)
	if len(missing) == 0 {
		return nil
	}
	return &MissingEnvVarsError{Names: missing}
}

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
