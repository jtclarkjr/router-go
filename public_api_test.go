package router_test

import (
	"io"
	"net"
	"net/http"
	"regexp"
	"time"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/middleware"
)

var (
	_ func() *router.Router                                                           = router.NewRouter
	_ string                                                                          = router.Version
	_ func(*router.Router, string, string, http.Handler) error                        = (*router.Router).Register
	_ func(*router.Router) []router.RouteInfo                                         = (*router.Router).Routes
	_ func(*http.Request, string) string                                              = router.URLParam
	_ func(*http.Request, string) string                                              = router.URLQuery
	_ http.Handler                                                                    = router.NewRouter()
	_ router.Middleware                                                               = func(next http.Handler) http.Handler { return next }
	_                                                                                 = router.Router{}
	_                                                                                 = router.Route{Handler: http.NotFoundHandler(), ParamKeys: []string{"id"}, ParamPattern: regexp.MustCompile("^/$")}
	_ middleware.WSHandler                                                            = func(net.Conn, *http.Request) {}
	_ func(middleware.WSHandler) func(http.Handler) http.Handler                      = middleware.WebSocket
	_ func(middleware.WSConfig, middleware.WSHandler) func(http.Handler) http.Handler = middleware.WebSocketWithConfig
	_ func(middleware.CORSConfig) func(http.Handler) http.Handler                     = middleware.CORS
	_ func() middleware.CORSConfig                                                    = middleware.DefaultCORSConfig
	_ func() func(http.Handler) http.Handler                                          = middleware.SimpleCORS
	_ func([]string) func(http.Handler) http.Handler                                  = middleware.StrictCORS
	_ func(http.Handler) http.Handler                                                 = middleware.Logger
	_ func(middleware.LoggerConfig) func(http.Handler) http.Handler                   = middleware.LoggerWithConfig
	_ func(http.Handler) http.Handler                                                 = middleware.RateLimiter
	_ func(http.Handler) http.Handler                                                 = middleware.Recoverer
	_ func(int) func(http.Handler) http.Handler                                       = middleware.Throttle
	_ func(...string) func(http.Handler) http.Handler                                 = middleware.EnvVarChecker
	_ func(int, time.Duration) *middleware.APIRateLimiter                             = middleware.NewAPIRateLimiter
	_ func() *middleware.SingleFlight                                                 = middleware.NewSingleFlight
	_ *middleware.APIRateLimiter                                                      = middleware.SharedAPIRateLimiter
	_ *http.Client                                                                    = middleware.SharedHTTPClient
	_ io.Writer                                                                       = middleware.LoggerConfig{}.Output
	_                                                                                 = middleware.ResponseWriterWrapper{}
	_                                                                                 = middleware.CORSConfig{}
	_                                                                                 = middleware.WSConfig{}
	_                                                                                 = middleware.APIRateLimiter{}
	_                                                                                 = middleware.SingleFlight{}
	_                                                                                 = middleware.Red
	_                                                                                 = middleware.Yellow
	_                                                                                 = middleware.Cyan
	_                                                                                 = middleware.Reset
)
