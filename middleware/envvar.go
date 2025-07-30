package middleware

import (
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
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Missing required environment variables: "))
				w.Write([]byte("[" + joinStrings(missing, ", ") + "]"))
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
