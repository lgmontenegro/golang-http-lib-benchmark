// Command dataservice owns the MySQL connection and serves transaction
// reads to upstream services. It exposes the same aggregate over both
// REST (HTTP+JSON) and gRPC, so the front can swap protocols without
// touching the application core.
//
// Stdlib net/http is used for the REST side deliberately — the
// dataservice is a dependency of the front, not a benchmark subject. We
// want a stable, framework-free floor so the measurement isolates
// front + protocol + dataservice without back-framework noise.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"example.com/httpdi/db/mysql"
	pb "example.com/httpdi/proto/transactionspb"
)

const defaultDSN = "httpdi:httpdipass@tcp(127.0.0.1:3306)/httpdi?parseTime=true"

func dsnFromEnv() string {
	if v := os.Getenv("DB_DSN"); v != "" {
		return v
	}
	return defaultDSN
}

func run() error {
	restAddr := flag.String("rest-addr", ":9090", "REST listen address")
	grpcAddr := flag.String("grpc-addr", ":9091", "gRPC listen address")
	dsn := flag.String("dsn", dsnFromEnv(), "MySQL DSN (or set DB_DSN)")
	flag.Parse()

	repo, err := mysql.New(*dsn)
	if err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	defer repo.Close()

	// REST
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /v1/transaction/{id}", makeGetTransactionHandler(repo))
	restSrv := &http.Server{Addr: *restAddr, Handler: mux}

	// gRPC
	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", *grpcAddr, err)
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterTransactionServiceServer(grpcSrv, newTransactionGRPCServer(repo))

	errCh := make(chan error, 2)
	go func() {
		log.Printf("dataservice REST on %s", *restAddr)
		errCh <- restSrv.ListenAndServe()
	}()
	go func() {
		log.Printf("dataservice gRPC on %s", *grpcAddr)
		errCh <- grpcSrv.Serve(lis)
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
	if err := restSrv.Shutdown(ctx); err != nil {
		log.Printf("rest shutdown: %v", err)
	}
	grpcSrv.GracefulStop()
	log.Println("dataservice exited cleanly")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
