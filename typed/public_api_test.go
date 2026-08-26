package typed_test

import (
	"net/http"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/typed"
)

var (
	_ func(*router.Router, ...typed.Option) *typed.Router                                                                                   = typed.New
	_ http.Handler                                                                                                                          = typed.New(nil)
	_ typed.ErrorCodec                                                                                                                      = typed.DefaultErrorCodec
	_ typed.ErrorCodec                                                                                                                      = typed.ErrorCodecFunc(nil)
	_ func(*typed.Router, typed.Operation[typed.Empty, typed.NoBody], typed.Handler[typed.Empty, typed.NoBody]) error                       = (*typed.Router).Register[typed.Empty, typed.NoBody]
	_ func(*typed.Router, typed.Operation[typed.Empty, typed.NoBody], typed.Handler[typed.Empty, typed.NoBody], ...router.Middleware) error = (*typed.Router).RegisterWithMiddleware[typed.Empty, typed.NoBody]
	_ func(*typed.Router, typed.Operation[typed.Empty, typed.NoBody], typed.Handler[typed.Empty, typed.NoBody])                             = (*typed.Router).MustRegister[typed.Empty, typed.NoBody]
	_ func(*typed.Router, typed.Operation[typed.Empty, typed.NoBody], typed.Handler[typed.Empty, typed.NoBody], ...router.Middleware)       = (*typed.Router).MustRegisterWithMiddleware[typed.Empty, typed.NoBody]
	_ func(*typed.Router, typed.RawOperation, http.Handler) error                                                                           = (*typed.Router).RegisterRaw
	_ func(*typed.Router, typed.RawOperation, http.Handler, ...router.Middleware) error                                                     = (*typed.Router).RegisterRawWithMiddleware
	_ func(*typed.Router, typed.RawOperation, http.Handler)                                                                                 = (*typed.Router).MustRegisterRaw
	_ func(*typed.Router, typed.RawOperation, http.Handler, ...router.Middleware)                                                           = (*typed.Router).MustRegisterRawWithMiddleware
	_                                                                                                                                       = typed.Register[typed.Empty, typed.NoBody]
	_                                                                                                                                       = typed.RegisterWithMiddleware[typed.Empty, typed.NoBody]
	_                                                                                                                                       = typed.MustRegister[typed.Empty, typed.NoBody]
	_                                                                                                                                       = typed.MustRegisterWithMiddleware[typed.Empty, typed.NoBody]
	_                                                                                                                                       = typed.RegisterRaw
	_                                                                                                                                       = typed.RegisterRawWithMiddleware
	_                                                                                                                                       = typed.MustRegisterRaw
	_                                                                                                                                       = typed.MustRegisterRawWithMiddleware
	_                                                                                                                                       = typed.WithRegistry
	_                                                                                                                                       = typed.WithErrorCodec
	_                                                                                                                                       = typed.WithMaxBodyBytes
	_                                                                                                                                       = typed.WithUnknownJSONFieldsAllowed
	_                                                                                                                                       = typed.Operation[typed.Empty, typed.NoBody]{}
	_                                                                                                                                       = typed.Response[typed.NoBody]{}
)
