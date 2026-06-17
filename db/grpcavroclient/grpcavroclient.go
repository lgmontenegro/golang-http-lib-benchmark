// Package grpcavroclient implements app.TransactionRepository by calling the
// dataservice's TransactionServiceAvro over gRPC with the Avro codec. It is
// the gRPC sibling of db/grpcclient (protobuf): same transport and framing,
// different serialization, so the benchmark can isolate protobuf vs Avro
// over gRPC. The front only sees app.TransactionRepository.
package grpcavroclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"example.com/httpdi/app"
	"example.com/httpdi/serde"
)

// avroGetByIDMethod is the full method name of the hand-written Avro service
// registered by cmd/dataservice (see grpc_avro.go).
const avroGetByIDMethod = "/httpdi.transactions.v1.TransactionServiceAvro/GetById"

// TransactionClient wraps a gRPC connection that exchanges Avro-encoded
// messages with the dataservice.
type TransactionClient struct {
	conn *grpc.ClientConn
}

// New dials target (e.g. "localhost:9091") with insecure credentials. Avro
// is selected per-call via content-subtype, not at dial time, so the same
// listener also serves the protobuf service.
func New(target string) (*TransactionClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc-avro dial %s: %w", target, err)
	}
	return &TransactionClient{conn: conn}, nil
}

// Close releases the underlying gRPC connection.
func (c *TransactionClient) Close() error {
	return c.conn.Close()
}

// GetByID invokes the Avro service, forcing the Avro codec via content-subtype.
// gRPC NotFound is translated back to app.ErrTransactionNotFound so the front's
// handler logic is identical to the other adapters.
func (c *TransactionClient) GetByID(ctx context.Context, id string) (app.Transaction, error) {
	req := &serde.AvroGetByIdRequest{ID: id}
	var resp serde.AvroTransaction
	err := c.conn.Invoke(ctx, avroGetByIDMethod, req, &resp, grpc.CallContentSubtype(serde.AvroCodecName))
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return app.Transaction{}, app.ErrTransactionNotFound
		}
		return app.Transaction{}, fmt.Errorf("grpc-avro get %s: %w", id, err)
	}
	return serde.AvroToDomain(resp), nil
}
