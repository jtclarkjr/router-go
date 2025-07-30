# Custom Go Router

## Overview
This package implements a custom router with middleware support for a web application, providing flexible routing and request handling.

## Why?
Q: Why not just use Chi for routing

A: Chi is really good and my favorite, but I was using custom routing per project that doesnt add extra things included in chi and just wanted to add it as a package to have in one place.

## Installation

```bash
go get github.com/jtclarkjr/router-go
```

## Usage

```go
func main() {
 // Create a new custom router
 r := router.NewRouter()

 // Middlewares
 r.Use(middleware.Logger)
 r.Use(middleware.Recoverer)
 r.Use(middleware.RateLimiter)
 r.Use(middleware.Throttle(5))

 // Food routes
 r.Get("/users", getUsersHandler)
 r.Post("/user", ceateUserHandler)
 r.Put("/users/{id}", createUserHandler)

 // Start the HTTP server
 fmt.Println("Starting server on :8080")
 log.Fatal(http.ListenAndServe(":8080", r))
}


// Example on how to use param in handler
// Extract id using URLParam
itemId := router.URLParam(r, "id")

```

Subroute example
```go
func main() {
  r := router.NewRouter()

  r.Route("/admin", func(r *router.Router) {
    r.Get("/users", getUsersHandler)
    r.Post("/users", createUserHandler)
  })
}

```

Two ways to use Query
```go
id := router.URLQuery(r, "id")
// or

// stlib net/http request.go approach
func Handler(w http.ResponseWriter, r *http.Request) {
  id := r.URL.Query().Get("id")
}

```

## Routing Features
- Supports HTTP methods
- Middleware chaining
- Dynamic route parameters
- Rate limiting and logging


## Middleware
- Logger: Logs incoming requests
- RateLimiter: Prevents excessive requests
- Throttle: Limits concurrent requests
- EnvVarChecker: Ensures required environment variables are set before handling requests

### Example: Using EnvVarChecker Middleware

```go
import (
    "github.com/jtclarkjr/router-go/middleware"
    // ...other imports
)

func main() {
    r := router.NewRouter()

    // Check that required environment variables are set
    r.Use(middleware.EnvVarChecker("DB_URL", "API_KEY"))

    // ...other middleware and routes
}
```

## Requirements
- Uses current latest Go version (1.24.1)
- Standard library packages

## License
MIT License
