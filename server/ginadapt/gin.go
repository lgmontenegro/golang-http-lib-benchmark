// Package ginadapt adapts Gin to the server.Server interface.
package ginadapt

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"example.com/httpdi/server"
)

// Adapter wraps a Gin engine.
type Adapter struct {
	engine *gin.Engine
	srv    *http.Server
}

// New returns a ready-to-use Gin adapter with the engine in release mode.
func New() *Adapter {
	gin.SetMode(gin.ReleaseMode)
	return &Adapter{engine: gin.New()}
}

// RegisterRoute binds a handler to the Gin router.
func (a *Adapter) RegisterRoute(method, path string, h server.HandlerFunc) {
	a.engine.Handle(method, path, func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Printf("gin: failed to read body: %v", err)
			c.String(http.StatusBadRequest, "failed to read request body")
			return
		}
		defer c.Request.Body.Close()

		params := make(map[string]string, len(c.Params))
		for _, p := range c.Params {
			params[p.Key] = p.Value
		}

		req := server.Request{
			Method: c.Request.Method,
			Path:   c.Request.URL.Path,
			Body:   body,
			Params: params,
		}

		resp := h(c.Request.Context(), req)

		for k, v := range resp.Headers {
			c.Header(k, v)
		}
		c.Data(resp.Status, "", resp.Body)
	})
}

// Start begins listening on the given address.
func (a *Adapter) Start(addr string) error {
	a.srv = &http.Server{Addr: addr, Handler: a.engine}
	return a.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (a *Adapter) Shutdown(ctx context.Context) error {
	return a.srv.Shutdown(ctx)
}
