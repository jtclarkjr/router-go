package middleware

import (
	"sync"
	"time"
)

// APIRateLimiter implements a token bucket algorithm for rate limiting API calls
type APIRateLimiter struct {
	tokens         int
	maxTokens      int
	refillRate     time.Duration
	mu             sync.Mutex
	lastRefillTime time.Time
}

// NewAPIRateLimiter creates a new rate limiter for API calls
// maxTokens: maximum number of tokens (burst capacity)
// refillRate: how often to add a token
func NewAPIRateLimiter(maxTokens int, refillRate time.Duration) *APIRateLimiter {
	return &APIRateLimiter{
		tokens:         maxTokens,
		maxTokens:      maxTokens,
		refillRate:     refillRate,
		lastRefillTime: time.Now(),
	}
}

// Allow checks if a request is allowed and consumes a token if so
func (rl *APIRateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(rl.lastRefillTime)
	tokensToAdd := int(elapsed / rl.refillRate)

	if tokensToAdd > 0 {
		rl.tokens += tokensToAdd
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefillTime = now
	}

	// Check if we have tokens available
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// Wait blocks until a token is available
func (rl *APIRateLimiter) Wait() {
	for !rl.Allow() {
		time.Sleep(10 * time.Millisecond)
	}
}

// Global rate limiter for all external API calls
// 100 requests per second maximum
var SharedAPIRateLimiter = NewAPIRateLimiter(100, 10*time.Millisecond)
