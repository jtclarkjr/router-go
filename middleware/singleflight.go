package middleware

import (
	"sync"
)

// call stores an in-flight or completed invocation.
type call struct {
	wg  sync.WaitGroup
	val []byte
	err error
}

// SingleFlight coalesces concurrent function calls with the same key.
type SingleFlight struct {
	mu sync.Mutex
	m  map[string]*call
}

// NewSingleFlight creates an initialized SingleFlight.
func NewSingleFlight() *SingleFlight {
	return &SingleFlight{
		m: make(map[string]*call),
	}
}

// Do runs fn once for concurrent callers sharing key.
// Waiting callers receive the same value and error.
func (sf *SingleFlight) Do(key string, fn func() ([]byte, error)) ([]byte, error) {
	sf.mu.Lock()
	if c, ok := sf.m[key]; ok {
		sf.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &call{}
	c.wg.Add(1)
	sf.m[key] = c
	sf.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	sf.mu.Lock()
	delete(sf.m, key)
	sf.mu.Unlock()

	return c.val, c.err
}
