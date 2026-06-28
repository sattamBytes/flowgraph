// Package gin is a minimal stub of github.com/gin-gonic/gin for hermetic
// resolver tests — only package path + method signatures matter.
package gin

type Context struct{}

type HandlerFunc func(*Context)

type Engine struct{}

func (e *Engine) GET(path string, h ...HandlerFunc)    {}
func (e *Engine) POST(path string, h ...HandlerFunc)   {}
func (e *Engine) PUT(path string, h ...HandlerFunc)    {}
func (e *Engine) DELETE(path string, h ...HandlerFunc) {}
func (e *Engine) PATCH(path string, h ...HandlerFunc)  {}

func Default() *Engine { return &Engine{} }
func New() *Engine     { return &Engine{} }
