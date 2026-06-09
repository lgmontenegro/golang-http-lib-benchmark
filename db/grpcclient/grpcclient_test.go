package grpcclient

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"example.com/httpdi/app"
	pb "example.com/httpdi/proto/transactionspb"
)

// fakeServer is a controllable TransactionServiceServer for tests.
type fakeServer struct {
	pb.UnimplementedTransactionServiceServer
	resp *pb.Transaction
	err  error
}

func (f *fakeServer) GetById(_ context.Context, _ *pb.GetByIdRequest) (*pb.Transaction, error) {
	return f.resp, f.err
}

// startTestServer spins up a real grpc.Server on an ephemeral TCP port
// with the given service impl, and returns the addr to dial. Real network
// loopback so the client exercises the full stack (HTTP/2, codec, status
// translation) rather than an in-memory shortcut.
func startTestServer(t *testing.T, impl pb.TransactionServiceServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	pb.RegisterTransactionServiceServer(gs, impl)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.GracefulStop() })
	return lis.Addr().String()
}

func TestGetByID(t *testing.T) {
	sample := app.Transaction{
		ID:         "abc",
		Value:      100.50,
		CreateDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Customer: app.Customer{
			ID:         "cust-1",
			Nome:       "Customer #1",
			CreateDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		CartSnapshot: app.CartSnapshot{
			ID:         "cart-1",
			CreateDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	}

	sampleProto := &pb.Transaction{
		Id:         sample.ID,
		Value:      sample.Value,
		CreateDate: timestamppb.New(sample.CreateDate),
		Customer: &pb.Customer{
			Id:         sample.Customer.ID,
			Nome:       sample.Customer.Nome,
			CreateDate: timestamppb.New(sample.Customer.CreateDate),
		},
		CartSnapshot: &pb.CartSnapshot{
			Id:         sample.CartSnapshot.ID,
			CreateDate: timestamppb.New(sample.CartSnapshot.CreateDate),
		},
	}

	t.Run("ok returns aggregate", func(t *testing.T) {
		addr := startTestServer(t, &fakeServer{resp: sampleProto})
		c, err := New(addr)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		got, err := c.GetByID(context.Background(), "abc")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if !reflect.DeepEqual(got, sample) {
			t.Errorf("got %+v\nwant %+v", got, sample)
		}
	})

	t.Run("NotFound returns ErrTransactionNotFound", func(t *testing.T) {
		addr := startTestServer(t, &fakeServer{err: status.Error(codes.NotFound, "missing")})
		c, err := New(addr)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		_, err = c.GetByID(context.Background(), "missing")
		if !errors.Is(err, app.ErrTransactionNotFound) {
			t.Errorf("err = %v, want ErrTransactionNotFound", err)
		}
	})

	t.Run("Internal returns wrapped non-sentinel error", func(t *testing.T) {
		addr := startTestServer(t, &fakeServer{err: status.Error(codes.Internal, "boom")})
		c, err := New(addr)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		_, err = c.GetByID(context.Background(), "x")
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if errors.Is(err, app.ErrTransactionNotFound) {
			t.Error("Internal must not be mapped to ErrTransactionNotFound")
		}
	})
}
