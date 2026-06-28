// Package chi is a minimal stub of github.com/go-chi/chi/v5 for hermetic
// resolver tests. Only the package path + method signatures the resolver keys
// on matter — this is not the real router.
package chi

import "net/http"

// Router is the subset of chi's interface the resolver recognizes.
type Router interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
	Delete(pattern string, h http.HandlerFunc)
	Patch(pattern string, h http.HandlerFunc)
	Head(pattern string, h http.HandlerFunc)
	Options(pattern string, h http.HandlerFunc)
}

// Mux is the concrete router returned by NewRouter.
type Mux struct{}

func (m *Mux) Get(pattern string, h http.HandlerFunc)     {}
func (m *Mux) Post(pattern string, h http.HandlerFunc)    {}
func (m *Mux) Put(pattern string, h http.HandlerFunc)     {}
func (m *Mux) Delete(pattern string, h http.HandlerFunc)  {}
func (m *Mux) Patch(pattern string, h http.HandlerFunc)   {}
func (m *Mux) Head(pattern string, h http.HandlerFunc)    {}
func (m *Mux) Options(pattern string, h http.HandlerFunc) {}

// NewRouter returns a stub router.
func NewRouter() *Mux { return &Mux{} }
