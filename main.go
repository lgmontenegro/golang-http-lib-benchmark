// Command httpdi is the front HTTP gateway. The -engine flag picks the
// HTTP framework (stdlib, gin, fiber, echo, chi) and the -repo flag picks
// the TransactionRepository implementation (mysql for in-process direct,
// or rest to delegate to the dataservice over HTTP+JSON).
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
	"example.com/httpdi/db/restclient"
	"example.com/httpdi/server"
	"example.com/httpdi/server/chiadapt"
	"example.com/httpdi/server/echoadapt"
	"example.com/httpdi/server/fiberadapt"
	"example.com/httpdi/server/ginadapt"
	"example.com/httpdi/server/stdlib"
)

const (
	defaultDSN      = "httpdi:httpdipass@tcp(127.0.0.1:3306)/httpdi?parseTime=true"
	defaultRepoAddr = "http://localhost:9090"
)

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

// newRepo builds the TransactionRepository selected by -repo and returns
// a cleanup func to defer. The mysql variant owns the connection pool;
// the rest variant has nothing to close (transient HTTP connections).
func newRepo(kind, dsn, repoAddr string) (app.TransactionRepository, func() error, error) {
	switch kind {
	case "rest":
		return restclient.New(repoAddr), func() error { return nil }, nil
	default: // "mysql"
		repo, err := mysql.New(dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("mysql: %w", err)
		}
		return repo, repo.Close, nil
	}
}

func run() error {
	engine := flag.String("engine", "stdlib", "HTTP engine: stdlib | gin | fiber | echo | chi")
	addr := flag.String("addr", ":8080", "listen address")
	repoKind := flag.String("repo", "mysql", "TransactionRepository: mysql | rest")
	dsn := flag.String("dsn", dsnFromEnv(), "MySQL DSN (used when -repo=mysql; also reads DB_DSN)")
	repoAddr := flag.String("repo-addr", defaultRepoAddr, "dataservice base URL (used when -repo=rest)")
	flag.Parse()

	txRepo, cleanup, err := newRepo(*repoKind, *dsn, *repoAddr)
	if err != nil {
		return err
	}
	defer func() {
		if err := cleanup(); err != nil {
			log.Printf("repo cleanup: %v", err)
		}
	}()

	application := app.New(txRepo)

	srv := newServer(*engine)
	srv.RegisterRoute("GET", "/health", application.Health)
	srv.RegisterRoute("GET", "/hello/:name", application.Hello)
	srv.RegisterRoute("GET", "/v1/transaction/:transaction_id", application.GetTransaction)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("starting front [engine=%s repo=%s] on %s", *engine, *repoKind, *addr)
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
