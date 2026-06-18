package grpcflatclient

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

	"example.com/httpdi/app"
	fb "example.com/httpdi/proto/transactionsfb"
	"example.com/httpdi/serde"
)

// fakeFlatServer is a controllable backend for the FlatBuffers gRPC service.
type fakeFlatServer struct {
	tx    app.Transaction
	batch []app.Transaction
	err   error
}

// flatServiceDesc mirrors cmd/dataservice's hand-written FlatBuffers service
// so the client can be tested against a real grpc.Server over loopback
// (HTTP/2 + the registered flatbuffers codec + status translation).
var flatServiceDesc = grpc.ServiceDesc{
	ServiceName: "httpdi.transactions.fb.TransactionServiceFlat",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetById",
			Handler: func(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(fb.GetByIdRequest)
				if err := dec(in); err != nil {
					return nil, err
				}
				f := srv.(*fakeFlatServer)
				if f.err != nil {
					return nil, f.err
				}
				return serde.MarshalFlatTransaction(f.tx), nil
			},
		},
		{
			MethodName: "GetBatch",
			Handler: func(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(fb.GetBatchRequest)
				if err := dec(in); err != nil {
					return nil, err
				}
				f := srv.(*fakeFlatServer)
				if f.err != nil {
					return nil, f.err
				}
				return serde.MarshalFlatList(f.batch), nil
			},
		},
	},
}

func startTestServer(t *testing.T, impl *fakeFlatServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&flatServiceDesc, impl)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.GracefulStop() })
	return lis.Addr().String()
}

func sample() app.Transaction {
	return app.Transaction{
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
}

func TestGetByID(t *testing.T) {
	want := sample()

	t.Run("ok returns aggregate", func(t *testing.T) {
		addr := startTestServer(t, &fakeFlatServer{tx: want})
		c, err := New(addr)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		got, err := c.GetByID(context.Background(), "abc")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})

	t.Run("NotFound returns sentinel", func(t *testing.T) {
		addr := startTestServer(t, &fakeFlatServer{err: status.Error(codes.NotFound, "missing")})
		c, _ := New(addr)
		t.Cleanup(func() { _ = c.Close() })
		_, err := c.GetByID(context.Background(), "missing")
		if !errors.Is(err, app.ErrTransactionNotFound) {
			t.Errorf("err = %v, want ErrTransactionNotFound", err)
		}
	})
}

func TestGetBatch(t *testing.T) {
	want := []app.Transaction{sample(), sample(), sample()}
	addr := startTestServer(t, &fakeFlatServer{batch: want})
	c, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	got, err := c.GetBatch(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}
