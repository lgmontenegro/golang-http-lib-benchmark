package main

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"example.com/httpdi/app"
	pb "example.com/httpdi/proto/transactionspb"
	"example.com/httpdi/serde"
)

// transactionGRPCServer implements the generated TransactionServiceServer
// on top of the same app.TransactionRepository that backs the REST handler.
type transactionGRPCServer struct {
	pb.UnimplementedTransactionServiceServer
	repo app.TransactionRepository
}

func newTransactionGRPCServer(repo app.TransactionRepository) *transactionGRPCServer {
	return &transactionGRPCServer{repo: repo}
}

// GetById fetches an aggregate by id. app.ErrTransactionNotFound is
// translated to gRPC codes.NotFound so the client can detect the sentinel.
func (s *transactionGRPCServer) GetById(ctx context.Context, req *pb.GetByIdRequest) (*pb.Transaction, error) {
	tx, err := s.repo.GetByID(ctx, req.GetId())
	if errors.Is(err, app.ErrTransactionNotFound) {
		return nil, status.Error(codes.NotFound, "transaction not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get transaction: %v", err)
	}
	return serde.TransactionToProto(tx), nil
}

// GetBatch fetches up to req.Limit aggregates and returns them as a
// TransactionList — the protobuf large-payload path.
func (s *transactionGRPCServer) GetBatch(ctx context.Context, req *pb.GetBatchRequest) (*pb.TransactionList, error) {
	txs, err := s.repo.GetBatch(ctx, int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get batch: %v", err)
	}
	list := &pb.TransactionList{Transactions: make([]*pb.Transaction, len(txs))}
	for i, tx := range txs {
		list.Transactions[i] = serde.TransactionToProto(tx)
	}
	return list, nil
}
