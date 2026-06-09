// Package echoadapt adapts Labstack Echo to the server.Server interface.
package echoadapt

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"

	"example.com/httpdi/server"
)

// Adapter wraps an Echo instance.
type Adapter struct {
	e   *echo.Echo
	srv *http.Server
}

// New returns a ready-to-use Echo adapter with banner and port logging
// suppressed so they don't pollute benchmark output.
func New() *Adapter {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	return &Adapter{e: e}
}

// RegisterRoute binds a handler to the Echo router. Echo natively uses the
// `:name` path-param syntax, so no translation is needed.
func (a *Adapter) RegisterRoute(method, path string, h server.HandlerFunc) {
	a.e.Add(method, path, func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			log.Printf("echo: failed to read body: %v", err)
			return c.String(http.StatusBadRequest, "failed to read request body")
		}
		defer c.Request().Body.Close()

		names := c.ParamNames()
		params := make(map[string]string, len(names))
		for _, name := range names {
			params[name] = c.Param(name)
		}

		req := server.Request{
			Method: c.Request().Method,
			Path:   c.Request().URL.Path,
			Body:   body,
			Params: params,
		}

		resp := h(c.Request().Context(), req)
		for k, v := range resp.Headers {
			c.Response().Header().Set(k, v)
		}
		c.Response().WriteHeader(resp.Status)
		if _, err := c.Response().Write(resp.Body); err != nil {
			log.Printf("echo: failed to write response: %v", err)
		}
		return nil
	})
}

// Start begins listening on the given address. Echo implements http.Handler,
// so we wrap it in a stdlib http.Server to get the same Shutdown semantics
// as the other adapters.
func (a *Adapter) Start(addr string) error {
	a.srv = &http.Server{Addr: addr, Handler: a.e}
	return a.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (a *Adapter) Shutdown(ctx context.Context) error {
	return a.srv.Shutdown(ctx)
}
