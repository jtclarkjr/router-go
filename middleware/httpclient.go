package middleware

import (
	"net/http"
	"time"
)

// SharedHTTPClient is a reusable HTTP client with connection pooling
var SharedHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	},
}
