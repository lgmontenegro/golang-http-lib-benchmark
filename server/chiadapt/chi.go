// Package chiadapt adapts go-chi to the server.Server interface.
package chiadapt

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/httpdi/server"
	"example.com/httpdi/server/internal/routeutil"
)

// Adapter wraps a chi.Router.
type Adapter struct {
	r   chi.Router
	srv *http.Server
}

// New returns a ready-to-use chi adapter.
func New() *Adapter {
	return &Adapter{r: chi.NewRouter()}
}

// RegisterRoute binds a handler to the chi router. chi uses `{name}`
// wildcards (the same shape as Go 1.22+ ServeMux), so we translate the
// canonical `:name` form through routeutil at register-time.
func (a *Adapter) RegisterRoute(method, path string, h server.HandlerFunc) {
	chiPath, paramNames := routeutil.TranslateColonToBrace(path)
	a.r.MethodFunc(method, chiPath, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("chi: failed to read body: %v", err)
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		params := make(map[string]string, len(paramNames))
		for _, name := range paramNames {
			params[name] = chi.URLParam(r, name)
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

// Start begins listening on the given address.
func (a *Adapter) Start(addr string) error {
	a.srv = &http.Server{Addr: addr, Handler: a.r}
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
		log.Printf("chi: failed to write response: %v", err)
	}
}
