# Custom Go Router

## Overview
This package implements a custom router with middleware support for a web application, providing flexible routing and request handling.

## Why?
Q: Why not just use Chi for routing

A: Chi is really good and my favorite, but was using custom routing per project that doesnt add extra things included in chi and just wanted to add it as a package to have in one place.

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
- CORS: Handles Cross-Origin Resource Sharing with flexible configuration

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

### CORS Middleware

The CORS middleware provides flexible configuration for handling Cross-Origin Resource Sharing.

#### Simple CORS (Allow all origins)

```go
import (
    "github.com/jtclarkjr/router-go/middleware"
)

func main() {
    r := router.NewRouter()
    
    // Allow all origins with default settings
    r.Use(middleware.SimpleCORS())
    
    // ...routes
}
```

#### Strict CORS (Specific origins only)

```go
func main() {
    r := router.NewRouter()
    
    // Only allow specific origins with credentials
    r.Use(middleware.StrictCORS([]string{
        "http://localhost:3000",
        "https://app.example.com",
    }))
    
    // ...routes
}
```

#### Custom CORS Configuration

```go
func main() {
    r := router.NewRouter()
    
    corsConfig := middleware.CORSConfig{
        // Origins that are allowed (supports wildcards)
        AllowedOrigins: []string{
            "http://localhost:3000",
            "https://example.com",
            "https://*.mydomain.com",  // Wildcard support
        },
        
        // HTTP methods that are allowed
        AllowedMethods: []string{
            http.MethodGet,
            http.MethodPost,
            http.MethodPut,
            http.MethodDelete,
            http.MethodOptions,
        },
        
        // Headers that the client can send
        AllowedHeaders: []string{
            "Content-Type",
            "Authorization",
            "X-Requested-With",
        },
        
        // Headers exposed to the client
        ExposedHeaders: []string{
            "X-Total-Count",
            "X-Page-Number",
        },
        
        // Cache preflight requests (in seconds)
        MaxAge: 3600,
        
        // Allow cookies/credentials
        AllowCredentials: true,
        
        // Let other handlers process OPTIONS
        OptionsPassthrough: false,
        
        // Enable debug headers
        Debug: true,
    }
    
    r.Use(middleware.CORS(corsConfig))
    
    // ...routes
}
```

#### CORS with Route Groups

You can apply different CORS configurations to different route groups:

```go
func main() {
    r := router.NewRouter()
    
    // Global CORS for all routes
    r.Use(middleware.SimpleCORS())
    
    // Admin routes with stricter CORS
    r.Route("/admin", func(admin *router.Router) {
        adminCORS := middleware.CORSConfig{
            AllowedOrigins:   []string{"https://admin.example.com"},
            AllowedMethods:   []string{http.MethodGet, http.MethodPost},
            AllowCredentials: true,
        }
        admin.Use(middleware.CORS(adminCORS))
        
        admin.Get("/dashboard", adminDashboardHandler)
    })
    
    // Public API routes
    r.Get("/api/public", publicAPIHandler)
}
```

#### CORS Configuration Options

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `AllowedOrigins` | `[]string` | List of allowed origins. Use `"*"` for all origins. Supports wildcards like `"https://*.example.com"` | `["*"]` |
| `AllowedMethods` | `[]string` | HTTP methods allowed for CORS requests | `[GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS]` |
| `AllowedHeaders` | `[]string` | Headers that can be used in requests. Use `"*"` for all headers | `["*"]` |
| `ExposedHeaders` | `[]string` | Headers exposed to the client | `[]` |
| `MaxAge` | `int` | How long (seconds) browsers can cache preflight responses | `0` |
| `AllowCredentials` | `bool` | Allow cookies, authorization headers, or TLS client certificates | `false` |
| `OptionsPassthrough` | `bool` | Pass OPTIONS requests to next handler instead of terminating | `false` |
| `Debug` | `bool` | Add X-CORS-Debug headers for troubleshooting | `false` |

## Requirements
- Uses current latest Go version (1.24.1)
- Standard library packages

## License
MIT License
