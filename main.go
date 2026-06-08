// Command httpdi demonstrates dependency inversion across HTTP frameworks.
// Use the -engine flag to select between stdlib, gin, and fiber.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/httpdi/server"
	"example.com/httpdi/server/fiberadapt"
	"example.com/httpdi/server/ginadapt"
	"example.com/httpdi/server/stdlib"
)

func newServer(engine string) server.Server {
	switch engine {
	case "gin":
		return ginadapt.New()
	case "fiber":
		return fiberadapt.New()
	default:
		return stdlib.New()
	}
}

func healthHandler(_ context.Context, _ server.Request) server.Response {
	return server.Response{
		Status:  200,
		Body:    []byte(`{"status":"ok"}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	}
}

func helloHandler(_ context.Context, req server.Request) server.Response {
	name := req.Params["name"]
	if name == "" {
		name = "world"
	}
	body := fmt.Sprintf(`{"hello":%q}`, name)
	return server.Response{
		Status:  200,
		Body:    []byte(body),
		Headers: map[string]string{"Content-Type": "application/json"},
	}
}

func run() error {
	engine := flag.String("engine", "stdlib", "HTTP engine: stdlib | gin | fiber")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	srv := newServer(*engine)
	srv.RegisterRoute("GET", "/health", healthHandler)
	srv.RegisterRoute("GET", "/hello/:name", helloHandler)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("starting [%s] on %s", *engine, *addr)
		errCh <- srv.Start(*addr)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Printf("received %v, shutting down", sig)
	case err := <-errCh:
		return fmt.Errorf("server failed: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Println("server exited cleanly")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
