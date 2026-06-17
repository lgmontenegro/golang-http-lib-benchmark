// Package grpcclient implements app.TransactionRepository by calling the
// dataservice's gRPC TransactionService. Drop-in replacement for db/mysql
// and db/restclient — the front only sees app.TransactionRepository, the
// protocol choice happens in main.go.
package grpcclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"example.com/httpdi/app"
	pb "example.com/httpdi/proto/transactionspb"
	"example.com/httpdi/serde"
)

// TransactionClient wraps a gRPC connection and exposes app's
// TransactionRepository over it.
type TransactionClient struct {
	conn   *grpc.ClientConn
	client pb.TransactionServiceClient
}

// New dials target (e.g. "localhost:9091") with insecure credentials —
// fine for benchmarks against a sibling service on the same host or LAN.
// Wrap with TLS credentials in any setup that crosses untrusted networks.
func New(target string) (*TransactionClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", target, err)
	}
	return &TransactionClient{
		conn:   conn,
		client: pb.NewTransactionServiceClient(conn),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *TransactionClient) Close() error {
	return c.conn.Close()
}

// GetByID translates the gRPC NotFound status back to
// app.ErrTransactionNotFound so the front's handler logic is identical
// to the in-process and REST paths.
func (c *TransactionClient) GetByID(ctx context.Context, id string) (app.Transaction, error) {
	resp, err := c.client.GetById(ctx, &pb.GetByIdRequest{Id: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return app.Transaction{}, app.ErrTransactionNotFound
		}
		return app.Transaction{}, fmt.Errorf("grpc get %s: %w", id, err)
	}
	return serde.TransactionFromProto(resp), nil
}
