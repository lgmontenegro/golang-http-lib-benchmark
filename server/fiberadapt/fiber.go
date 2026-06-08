// Package fiberadapt adapts Fiber to the server.Server interface.
package fiberadapt

import (
	"context"

	"github.com/gofiber/fiber/v2"

	"example.com/httpdi/server"
)

// Adapter wraps a Fiber app.
type Adapter struct {
	app *fiber.App
}

// New returns a ready-to-use Fiber adapter.
func New() *Adapter {
	return &Adapter{
		app: fiber.New(fiber.Config{
			DisableStartupMessage: true,
		}),
	}
}

// RegisterRoute binds a handler to the Fiber router.
func (a *Adapter) RegisterRoute(method, path string, h server.HandlerFunc) {
	a.app.Add(method, path, func(c *fiber.Ctx) error {
		params := make(map[string]string)
		for _, key := range c.Route().Params {
			params[key] = c.Params(key)
		}

		// Fiber reuses the body buffer across requests, so a copy
		// is required to avoid data races in concurrent handlers.
		body := make([]byte, len(c.Body()))
		copy(body, c.Body())

		req := server.Request{
			Method: c.Method(),
			Path:   c.Path(),
			Body:   body,
			Params: params,
		}

		resp := h(c.UserContext(), req)

		for k, v := range resp.Headers {
			c.Set(k, v)
		}
		return c.Status(resp.Status).Send(resp.Body)
	})
}

// Start begins listening on the given address.
func (a *Adapter) Start(addr string) error {
	return a.app.Listen(addr)
}

// Shutdown gracefully stops the server.
func (a *Adapter) Shutdown(_ context.Context) error {
	return a.app.Shutdown()
}
