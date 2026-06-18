package grpcavroclient

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
	"example.com/httpdi/serde"
)

// fakeAvroServer is a controllable backend for the Avro gRPC service.
type fakeAvroServer struct {
	resp  serde.AvroTransaction
	batch []serde.AvroTransaction
	err   error
}

// avroServiceDesc mirrors cmd/dataservice's hand-written Avro service so the
// client can be tested against a real grpc.Server over loopback (HTTP/2 +
// the registered Avro codec + status translation), not an in-memory shim.
var avroServiceDesc = grpc.ServiceDesc{
	ServiceName: "httpdi.transactions.v1.TransactionServiceAvro",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetById",
			Handler: func(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(serde.AvroGetByIdRequest)
				if err := dec(in); err != nil {
					return nil, err
				}
				f := srv.(*fakeAvroServer)
				if f.err != nil {
					return nil, f.err
				}
				resp := f.resp
				return &resp, nil
			},
		},
		{
			MethodName: "GetBatch",
			Handler: func(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(serde.AvroGetBatchRequest)
				if err := dec(in); err != nil {
					return nil, err
				}
				f := srv.(*fakeAvroServer)
				if f.err != nil {
					return nil, f.err
				}
				batch := f.batch
				return &batch, nil
			},
		},
	},
}

func startTestServer(t *testing.T, impl *fakeAvroServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	gs.RegisterService(&avroServiceDesc, impl)
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

	t.Run("ok returns aggregate", func(t *testing.T) {
		addr := startTestServer(t, &fakeAvroServer{resp: serde.DomainToAvro(sample)})
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
		addr := startTestServer(t, &fakeAvroServer{err: status.Error(codes.NotFound, "missing")})
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
		addr := startTestServer(t, &fakeAvroServer{err: status.Error(codes.Internal, "boom")})
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

	t.Run("GetBatch returns the list", func(t *testing.T) {
		want := []app.Transaction{sample, sample}
		addr := startTestServer(t, &fakeAvroServer{batch: []serde.AvroTransaction{serde.DomainToAvro(sample), serde.DomainToAvro(sample)}})
		c, err := New(addr)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		got, err := c.GetBatch(context.Background(), 2)
		if err != nil {
			t.Fatalf("GetBatch: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
}
