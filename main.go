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

	"example.com/httpdi/app"
	"example.com/httpdi/db/mysql"
	"example.com/httpdi/server"
	"example.com/httpdi/server/chiadapt"
	"example.com/httpdi/server/echoadapt"
	"example.com/httpdi/server/fiberadapt"
	"example.com/httpdi/server/ginadapt"
	"example.com/httpdi/server/stdlib"
)

const defaultDSN = "httpdi:httpdipass@tcp(127.0.0.1:3306)/httpdi?parseTime=true"

func dsnFromEnv() string {
	if v := os.Getenv("DB_DSN"); v != "" {
		return v
	}
	return defaultDSN
}

func newServer(engine string) server.Server {
	switch engine {
	case "gin":
		return ginadapt.New()
	case "fiber":
		return fiberadapt.New()
	case "echo":
		return echoadapt.New()
	case "chi":
		return chiadapt.New()
	default:
		return stdlib.New()
	}
}

func run() error {
	engine := flag.String("engine", "stdlib", "HTTP engine: stdlib | gin | fiber | echo | chi")
	addr := flag.String("addr", ":8080", "listen address")
	dsn := flag.String("dsn", dsnFromEnv(), "MySQL DSN (or set DB_DSN)")
	flag.Parse()

	txRepo, err := mysql.New(*dsn)
	if err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	defer txRepo.Close()

	application := app.New(txRepo)

	srv := newServer(*engine)
	srv.RegisterRoute("GET", "/health", application.Health)
	srv.RegisterRoute("GET", "/hello/:name", application.Hello)
	srv.RegisterRoute("GET", "/v1/transaction/:transaction_id", application.GetTransaction)

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
