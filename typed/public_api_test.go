package typed_test

import (
	"net/http"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/typed"
)

var (
	_ func(*router.Router, ...typed.Option) *typed.Router = typed.New
	_ http.Handler                                        = typed.New(nil)
	_ typed.ErrorCodec                                    = typed.DefaultErrorCodec
	_ typed.ErrorCodec                                    = typed.ErrorCodecFunc(nil)
	_                                                     = typed.Register[typed.Empty, typed.NoBody]
	_                                                     = typed.RegisterWithMiddleware[typed.Empty, typed.NoBody]
	_                                                     = typed.MustRegister[typed.Empty, typed.NoBody]
	_                                                     = typed.MustRegisterWithMiddleware[typed.Empty, typed.NoBody]
	_                                                     = typed.RegisterRaw
	_                                                     = typed.RegisterRawWithMiddleware
	_                                                     = typed.MustRegisterRaw
	_                                                     = typed.MustRegisterRawWithMiddleware
	_                                                     = typed.WithRegistry
	_                                                     = typed.WithErrorCodec
	_                                                     = typed.WithMaxBodyBytes
	_                                                     = typed.WithUnknownJSONFieldsAllowed
	_                                                     = typed.Operation[typed.Empty, typed.NoBody]{}
	_                                                     = typed.Response[typed.NoBody]{}
)
