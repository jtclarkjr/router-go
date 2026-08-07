package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter allows one request per remote address per second.
func RateLimiter(next http.Handler) http.Handler {
	lastRequestTime := make(map[string]time.Time)
	var mu sync.Mutex

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		mu.Lock()
		defer mu.Unlock()

		now := time.Now()
		if lastTime, exists := lastRequestTime[clientIP]; exists {
			if now.Sub(lastTime) < time.Second {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
		}
		lastRequestTime[clientIP] = now
		next.ServeHTTP(w, r)
	})
}
