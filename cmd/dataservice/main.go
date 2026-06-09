// Command dataservice owns the MySQL connection and serves transaction
// reads to upstream services. It exposes the same shape as the front's
// /v1/transaction/:id endpoint, so the front can swap its in-process
// MySQL adapter for a network client (REST or gRPC) without touching the
// application core.
//
// Stdlib net/http is used deliberately — the dataservice is a dependency
// of the front, not a benchmark subject. We want a stable, framework-free
// floor so we measure the front + protocol + dataservice without any
// framework-specific noise on the back.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/httpdi/db/mysql"
)

const defaultDSN = "httpdi:httpdipass@tcp(127.0.0.1:3306)/httpdi?parseTime=true"

func dsnFromEnv() string {
	if v := os.Getenv("DB_DSN"); v != "" {
		return v
	}
	return defaultDSN
}

func run() error {
	addr := flag.String("addr", ":9090", "REST listen address")
	dsn := flag.String("dsn", dsnFromEnv(), "MySQL DSN (or set DB_DSN)")
	flag.Parse()

	repo, err := mysql.New(*dsn)
	if err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	defer repo.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/transaction/{id}", makeGetTransactionHandler(repo))

	srv := &http.Server{Addr: *addr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("dataservice REST on %s", *addr)
		errCh <- srv.ListenAndServe()
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
	log.Println("dataservice exited cleanly")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
