# Custom Go Router

## Overview
This package implements a custom router with middleware support for a web application, providing flexible routing and request handling.

## Why?
Q: Why not just use Chi for routing

A: Chi is really good and my favorite, but I was using custom routing per project and just wanted to add it as a package to have in one place.

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
 r.Use(middleware.RateLimiter)
 r.Use(middleware.Throttle(5))

 // Food routes
 r.Get("/users", GetUsersHandler)
 r.Post("/user", controller.CreateUserHandler)
 r.Put("/users/{id}", controller.UpdateUserHandler)

 // Start the HTTP server
 fmt.Println("Starting server on :8080")
 log.Fatal(http.ListenAndServe(":8080", r))
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

## Requirements
- Uses current latest Go version (1.24.1)
- Standard library packages

## License
MIT License
