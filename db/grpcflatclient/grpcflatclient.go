// Package grpcflatclient implements app.TransactionRepository by calling the
// dataservice's TransactionServiceFlat over gRPC with the FlatBuffers codec.
// It is the zero-copy sibling of db/grpcclient (protobuf) and db/grpcavroclient
// (Avro): same transport and framing, different serialization, so the
// benchmark can isolate FlatBuffers against the others. The front only sees
// app.TransactionRepository.
package grpcflatclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"example.com/httpdi/app"
	fb "example.com/httpdi/proto/transactionsfb"
	"example.com/httpdi/serde"
)

// Full method names of the hand-written FlatBuffers service registered by
// cmd/dataservice (see grpc_flat.go).
const (
	flatGetByIDMethod = "/httpdi.transactions.fb.TransactionServiceFlat/GetById"
	flatGetBatchMethod = "/httpdi.transactions.fb.TransactionServiceFlat/GetBatch"
)

// TransactionClient wraps a gRPC connection that exchanges FlatBuffers
// messages with the dataservice.
type TransactionClient struct {
	conn *grpc.ClientConn
}

// New dials target (e.g. "localhost:9091") with insecure credentials.
// FlatBuffers is selected per-call via content-subtype.
func New(target string) (*TransactionClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc-flat dial %s: %w", target, err)
	}
	return &TransactionClient{conn: conn}, nil
}

// Close releases the underlying gRPC connection.
func (c *TransactionClient) Close() error {
	return c.conn.Close()
}

// GetByID invokes the FlatBuffers service, forcing the flatbuffers codec.
// gRPC NotFound is translated back to app.ErrTransactionNotFound.
func (c *TransactionClient) GetByID(ctx context.Context, id string) (app.Transaction, error) {
	req := serde.MarshalFlatGetByID(id)
	var resp fb.Transaction
	err := c.conn.Invoke(ctx, flatGetByIDMethod, req, &resp, grpc.CallContentSubtype(serde.FlatCodecName))
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return app.Transaction{}, app.ErrTransactionNotFound
		}
		return app.Transaction{}, fmt.Errorf("grpc-flat get %s: %w", id, err)
	}
	return serde.FlatToDomain(&resp), nil
}

// GetBatch fetches up to limit aggregates over the FlatBuffers gRPC service.
func (c *TransactionClient) GetBatch(ctx context.Context, limit int) ([]app.Transaction, error) {
	req := serde.MarshalFlatGetBatch(limit)
	var resp fb.TransactionList
	err := c.conn.Invoke(ctx, flatGetBatchMethod, req, &resp, grpc.CallContentSubtype(serde.FlatCodecName))
	if err != nil {
		return nil, fmt.Errorf("grpc-flat batch (limit=%d): %w", limit, err)
	}
	return serde.FlatListToDomain(&resp), nil
}
