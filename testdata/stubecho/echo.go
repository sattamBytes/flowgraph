// Package echo is a minimal stub of github.com/labstack/echo/v4 for hermetic
// resolver tests — only package path + method signatures matter.
package echo

type Context interface{}

type HandlerFunc func(Context) error

type MiddlewareFunc func(HandlerFunc) HandlerFunc

type Route struct{}

type Echo struct{}

func (e *Echo) GET(path string, h HandlerFunc, m ...MiddlewareFunc) *Route    { return nil }
func (e *Echo) POST(path string, h HandlerFunc, m ...MiddlewareFunc) *Route   { return nil }
func (e *Echo) PUT(path string, h HandlerFunc, m ...MiddlewareFunc) *Route    { return nil }
func (e *Echo) DELETE(path string, h HandlerFunc, m ...MiddlewareFunc) *Route { return nil }

func New() *Echo { return &Echo{} }
