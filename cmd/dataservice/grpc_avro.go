package main

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"example.com/httpdi/app"
	"example.com/httpdi/serde"
)

// transactionAvroServiceDesc is a hand-written gRPC service that carries the
// aggregate as Avro instead of protobuf. The generated TransactionService is
// locked to the protobuf codec, so isolating "protobuf vs Avro" over the same
// gRPC framing needs a parallel service whose messages are the serde Avro
// DTOs. The client selects it per-call with grpc.CallContentSubtype("avro").
var transactionAvroServiceDesc = grpc.ServiceDesc{
	ServiceName: "httpdi.transactions.v1.TransactionServiceAvro",
	HandlerType: (*app.TransactionRepository)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetById", Handler: getByIDAvroHandler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "serde/avro",
}

// registerTransactionAvroServer wires the Avro gRPC service onto an existing
// gRPC server, backed by the same repository as the protobuf and REST paths.
func registerTransactionAvroServer(s *grpc.Server, repo app.TransactionRepository) {
	s.RegisterService(&transactionAvroServiceDesc, repo)
}

func getByIDAvroHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(serde.AvroGetByIdRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	repo := srv.(app.TransactionRepository)
	handler := func(ctx context.Context, req any) (any, error) {
		tx, err := repo.GetByID(ctx, req.(*serde.AvroGetByIdRequest).ID)
		if errors.Is(err, app.ErrTransactionNotFound) {
			return nil, status.Error(codes.NotFound, "transaction not found")
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get transaction: %v", err)
		}
		resp := serde.DomainToAvro(tx)
		return &resp, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/httpdi.transactions.v1.TransactionServiceAvro/GetById"}
	return interceptor(ctx, in, info, handler)
}
