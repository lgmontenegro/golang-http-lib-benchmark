package main

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"example.com/httpdi/app"
	pb "example.com/httpdi/proto/transactionspb"
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
	return transactionToProto(tx), nil
}

func transactionToProto(tx app.Transaction) *pb.Transaction {
	return &pb.Transaction{
		Id:         tx.ID,
		Value:      tx.Value,
		CreateDate: timestamppb.New(tx.CreateDate),
		Customer: &pb.Customer{
			Id:         tx.Customer.ID,
			Nome:       tx.Customer.Nome,
			CreateDate: timestamppb.New(tx.Customer.CreateDate),
		},
		CartSnapshot: &pb.CartSnapshot{
			Id:         tx.CartSnapshot.ID,
			CreateDate: timestamppb.New(tx.CartSnapshot.CreateDate),
		},
	}
}
