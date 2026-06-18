// Command dataservice owns the MySQL connection and serves transaction
// reads to upstream services. It exposes the same aggregate over three
// transports so the front can swap them without touching the application
// core:
//   - REST/HTTP1   on -rest-addr     (content-negotiated JSON/protobuf/Avro)
//   - REST/HTTP2   on -rest-h2c-addr (h2c, same content negotiation)
//   - gRPC         on -grpc-addr     (protobuf and Avro via content-subtype)
//
// Stdlib net/http is used for the REST side deliberately — the
// dataservice is a dependency of the front, not a benchmark subject. We
// want a stable, framework-free floor so the measurement isolates
// front + protocol + serialization without back-framework noise.
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

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
	restAddr := flag.String("rest-addr", ":9090", "REST HTTP/1.1 listen address")
	restH2CAddr := flag.String("rest-h2c-addr", ":9092", "REST HTTP/2 cleartext (h2c) listen address")
	grpcAddr := flag.String("grpc-addr", ":9091", "gRPC listen address")
	dsn := flag.String("dsn", dsnFromEnv(), "MySQL DSN (or set DB_DSN)")
	flag.Parse()

	repo, err := mysql.New(*dsn)
	if err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	defer repo.Close()

	// REST — one mux serves both the HTTP/1.1 and the h2c server; the
	// handler picks the response format from the Accept header.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /v1/transaction/{id}", makeGetTransactionHandler(repo))
	mux.HandleFunc("GET /v1/transactions/{count}", makeGetBatchHandler(repo))
	restSrv := &http.Server{Addr: *restAddr, Handler: mux}
	h2cSrv := &http.Server{Addr: *restH2CAddr, Handler: h2c.NewHandler(mux, &http2.Server{})}

	// gRPC — protobuf via the generated service, Avro via a parallel service
	// selected by content-subtype on the same listener.
	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", *grpcAddr, err)
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterTransactionServiceServer(grpcSrv, newTransactionGRPCServer(repo))
	registerTransactionAvroServer(grpcSrv, repo)
	registerTransactionFlatServer(grpcSrv, repo)

	errCh := make(chan error, 3)
	go func() {
		log.Printf("dataservice REST on %s", *restAddr)
		errCh <- restSrv.ListenAndServe()
	}()
	go func() {
		log.Printf("dataservice REST h2c on %s", *restH2CAddr)
		errCh <- h2cSrv.ListenAndServe()
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
	if err := h2cSrv.Shutdown(ctx); err != nil {
		log.Printf("rest h2c shutdown: %v", err)
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
