// Package server defines the contract for HTTP server implementations.
// It enables dependency inversion by letting the domain declare the
// interface while concrete frameworks provide the implementation.
package server

import "context"

// Request represents an inbound HTTP request in a framework-agnostic way.
type Request struct {
	Method string
	Path   string
	Body   []byte
	Params map[string]string
}

// Response represents an outbound HTTP response.
type Response struct {
	Status  int
	Body    []byte
	Headers map[string]string
}

// HandlerFunc is the unified signature for any HTTP handler,
// independent of the underlying framework.
type HandlerFunc func(ctx context.Context, req Request) Response

// Server is the contract that any HTTP framework must satisfy.
type Server interface {
	// RegisterRoute binds a handler to the given HTTP method and path.
	RegisterRoute(method, path string, h HandlerFunc)

	// Start begins listening on addr. It blocks until the server stops.
	Start(addr string) error

	// Shutdown performs a graceful shutdown within the given context deadline.
	Shutdown(ctx context.Context) error
}
