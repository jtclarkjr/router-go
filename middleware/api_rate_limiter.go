package middleware

import (
	"sync"
	"time"
)

// APIRateLimiter implements a token-bucket rate limiter for API calls.
type APIRateLimiter struct {
	tokens         int
	maxTokens      int
	refillRate     time.Duration
	mu             sync.Mutex
	lastRefillTime time.Time
}

// NewAPIRateLimiter creates a token-bucket limiter with maxTokens capacity.
// refillRate controls how often one token is restored.
func NewAPIRateLimiter(maxTokens int, refillRate time.Duration) *APIRateLimiter {
	return &APIRateLimiter{
		tokens:         maxTokens,
		maxTokens:      maxTokens,
		refillRate:     refillRate,
		lastRefillTime: time.Now(),
	}
}

// Allow consumes one token when one is available.
func (rl *APIRateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

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

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// Wait blocks until it can consume a token.
func (rl *APIRateLimiter) Wait() {
	for !rl.Allow() {
		time.Sleep(10 * time.Millisecond)
	}
}

// SharedAPIRateLimiter limits shared external API calls to approximately 100 per second.
var SharedAPIRateLimiter = NewAPIRateLimiter(100, 10*time.Millisecond)
