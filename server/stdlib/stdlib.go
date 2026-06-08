// Package stdlib adapts net/http to the server.Server interface.
package stdlib

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"example.com/httpdi/server"
)

// Adapter wraps the standard library HTTP server.
type Adapter struct {
	mux *http.ServeMux
	srv *http.Server
}

// New returns a ready-to-use standard library adapter.
func New() *Adapter {
	return &Adapter{mux: http.NewServeMux()}
}

// RegisterRoute binds a handler using the Go 1.22+ "METHOD /path" pattern.
// Path params are accepted in the shared Gin/Fiber `:name` form and rewritten
// to ServeMux's `{name}` wildcards so the same route string works across all
// three adapters.
func (a *Adapter) RegisterRoute(method, path string, h server.HandlerFunc) {
	stdPath, paramNames := translatePath(path)
	pattern := fmt.Sprintf("%s %s", method, stdPath)
	a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("stdlib: failed to read body: %v", err)
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		params := make(map[string]string, len(paramNames))
		for _, name := range paramNames {
			params[name] = r.PathValue(name)
		}

		req := server.Request{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   body,
			Params: params,
		}

		resp := h(r.Context(), req)
		writeResponse(w, resp)
	})
}

// translatePath converts `/hello/:name` into `/hello/{name}` and returns the
// list of param names in order of appearance.
func translatePath(path string) (string, []string) {
	segments := strings.Split(path, "/")
	var names []string
	for i, seg := range segments {
		if strings.HasPrefix(seg, ":") && len(seg) > 1 {
			name := seg[1:]
			names = append(names, name)
			segments[i] = "{" + name + "}"
		}
	}
	return strings.Join(segments, "/"), names
}

// Start begins listening on the given address.
func (a *Adapter) Start(addr string) error {
	a.srv = &http.Server{Addr: addr, Handler: a.mux}
	return a.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (a *Adapter) Shutdown(ctx context.Context) error {
	return a.srv.Shutdown(ctx)
}

func writeResponse(w http.ResponseWriter, resp server.Response) {
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(resp.Status)
	if _, err := w.Write(resp.Body); err != nil {
		log.Printf("stdlib: failed to write response: %v", err)
	}
}
