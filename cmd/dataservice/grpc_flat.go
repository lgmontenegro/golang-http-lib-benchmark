package main

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"example.com/httpdi/app"
	fb "example.com/httpdi/proto/transactionsfb"
	"example.com/httpdi/serde"
)

// transactionFlatServiceDesc is the FlatBuffers (zero-copy) sibling of the
// protobuf and Avro gRPC services — same framing, different serialization,
// so the benchmark can isolate the wire format. Requests arrive as generated
// FlatBuffers tables and responses leave as finished *flatbuffers.Builder,
// per the FlatBuffers gRPC convention. Selected per-call with
// grpc.CallContentSubtype("flatbuffers").
var transactionFlatServiceDesc = grpc.ServiceDesc{
	ServiceName: "httpdi.transactions.fb.TransactionServiceFlat",
	HandlerType: (*app.TransactionRepository)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetById", Handler: getByIDFlatHandler},
		{MethodName: "GetBatch", Handler: getBatchFlatHandler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "serde/flatbuffers",
}

// registerTransactionFlatServer wires the FlatBuffers gRPC service onto an
// existing gRPC server, backed by the same repository as the other paths.
func registerTransactionFlatServer(s *grpc.Server, repo app.TransactionRepository) {
	s.RegisterService(&transactionFlatServiceDesc, repo)
}

func getByIDFlatHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(fb.GetByIdRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	repo := srv.(app.TransactionRepository)
	handler := func(ctx context.Context, req any) (any, error) {
		tx, err := repo.GetByID(ctx, string(req.(*fb.GetByIdRequest).Id()))
		if errors.Is(err, app.ErrTransactionNotFound) {
			return nil, status.Error(codes.NotFound, "transaction not found")
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get transaction: %v", err)
		}
		return serde.MarshalFlatTransaction(tx), nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/httpdi.transactions.fb.TransactionServiceFlat/GetById"}
	return interceptor(ctx, in, info, handler)
}

func getBatchFlatHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(fb.GetBatchRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	repo := srv.(app.TransactionRepository)
	handler := func(ctx context.Context, req any) (any, error) {
		txs, err := repo.GetBatch(ctx, int(req.(*fb.GetBatchRequest).Limit()))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get batch: %v", err)
		}
		return serde.MarshalFlatList(txs), nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/httpdi.transactions.fb.TransactionServiceFlat/GetBatch"}
	return interceptor(ctx, in, info, handler)
}
