package middleware

import (
	"net/http"
)

// Throttle limits the number of concurrent requests.
func Throttle(limit int) func(http.Handler) http.Handler {
	sem := make(chan struct{}, limit)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Acquire a token
			sem <- struct{}{}
			defer func() {
				// Release the token
				<-sem
			}()

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}
