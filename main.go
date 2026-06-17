// Command httpdi is the front HTTP gateway. The -engine flag picks the
// HTTP framework (stdlib, gin, fiber, echo, chi) and the -repo flag picks
// the TransactionRepository implementation — a (transport, serialization)
// pair so the benchmark can isolate each axis:
//   mysql       — in-process via sqlx (the default).
//   rest        — dataservice over HTTP/1.1 + JSON.
//   resth2      — dataservice over HTTP/2 (h2c) + JSON.
//   resth2-pb   — dataservice over HTTP/2 (h2c) + protobuf.
//   resth2-avro — dataservice over HTTP/2 (h2c) + Avro.
//   grpc        — dataservice over gRPC + protobuf.
//   grpc-avro   — dataservice over gRPC + Avro.
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
	"example.com/httpdi/db/grpcavroclient"
	"example.com/httpdi/db/grpcclient"
	"example.com/httpdi/db/mysql"
	"example.com/httpdi/db/restclient"
	"example.com/httpdi/serde"
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

// noClose is the cleanup for stateless adapters (the REST clients keep no
// long-lived resource the caller must release).
func noClose() error { return nil }

// newRepo builds the TransactionRepository selected by -repo and returns
// a cleanup func to defer. mysql owns the connection pool; the rest* clients
// are stateless HTTP clients (nothing to close); the grpc* clients own a
// long-lived ClientConn that must be closed.
func newRepo(kind, dsn, repoAddr string) (app.TransactionRepository, func() error, error) {
	switch kind {
	case "rest":
		return restclient.New(repoAddr), noClose, nil
	case "resth2":
		return restclient.NewHTTP2(repoAddr, serde.JSON), noClose, nil
	case "resth2-pb":
		return restclient.NewHTTP2(repoAddr, serde.Protobuf), noClose, nil
	case "resth2-avro":
		return restclient.NewHTTP2(repoAddr, serde.Avro), noClose, nil
	case "grpc":
		client, err := grpcclient.New(repoAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("grpc: %w", err)
		}
		return client, client.Close, nil
	case "grpc-avro":
		client, err := grpcavroclient.New(repoAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("grpc-avro: %w", err)
		}
		return client, client.Close, nil
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
	repoKind := flag.String("repo", "mysql", "TransactionRepository: mysql | rest | resth2 | resth2-pb | resth2-avro | grpc | grpc-avro")
	dsn := flag.String("dsn", dsnFromEnv(), "MySQL DSN (used when -repo=mysql; also reads DB_DSN)")
	repoAddr := flag.String("repo-addr", defaultRepoAddr, "dataservice address (URL for rest, host:port for grpc; e.g. localhost:9091)")
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
