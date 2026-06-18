package restclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"example.com/httpdi/app"
	"example.com/httpdi/serde"
)

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

	t.Run("200 returns aggregate", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sample)
		}))
		defer srv.Close()

		c := New(srv.URL)
		got, err := c.GetByID(context.Background(), "abc")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if !reflect.DeepEqual(got, sample) {
			t.Errorf("got %+v\nwant %+v", got, sample)
		}
		if gotPath != "/v1/transaction/abc" {
			t.Errorf("server saw path %q, want /v1/transaction/abc", gotPath)
		}
	})

	t.Run("404 returns ErrTransactionNotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		c := New(srv.URL)
		_, err := c.GetByID(context.Background(), "missing")
		if !errors.Is(err, app.ErrTransactionNotFound) {
			t.Errorf("err = %v, want ErrTransactionNotFound", err)
		}
	})

	t.Run("5xx returns wrapped error, not the sentinel", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := New(srv.URL)
		_, err := c.GetByID(context.Background(), "x")
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if errors.Is(err, app.ErrTransactionNotFound) {
			t.Error("5xx must not be mapped to ErrTransactionNotFound")
		}
	})

	t.Run("ID with special chars is url-escaped", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.EscapedPath()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sample)
		}))
		defer srv.Close()

		c := New(srv.URL)
		if _, err := c.GetByID(context.Background(), "a b/c"); err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if gotPath != "/v1/transaction/a%20b%2Fc" {
			t.Errorf("escaped path = %q, want /v1/transaction/a%%20b%%2Fc", gotPath)
		}
	})
}

// newH2CServer starts an httptest server that speaks HTTP/2 cleartext and
// content-negotiates the body via serde, mirroring the real dataservice.
// It records the negotiated protocol so the test can assert HTTP/2 was used.
func newH2CServer(t *testing.T, tx app.Transaction, proto *int) *httptest.Server {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*proto = r.ProtoMajor
		codec := serde.ForAccept(r.Header.Get("Accept"))
		body, err := codec.Marshal(tx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", codec.ContentType())
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(h2c.NewHandler(h, &http2.Server{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetByID_HTTP2(t *testing.T) {
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

	codecs := map[string]serde.Codec{
		"json":     serde.JSON,
		"protobuf": serde.Protobuf,
		"avro":     serde.Avro,
	}
	for name, codec := range codecs {
		t.Run(name+" 200 over HTTP/2", func(t *testing.T) {
			var proto int
			srv := newH2CServer(t, sample, &proto)

			c := NewHTTP2(srv.URL, codec)
			got, err := c.GetByID(context.Background(), "abc")
			if err != nil {
				t.Fatalf("GetByID: %v", err)
			}
			if proto != 2 {
				t.Errorf("server saw HTTP/%d, want HTTP/2", proto)
			}
			if !reflect.DeepEqual(got, sample) {
				t.Errorf("got %+v\nwant %+v", got, sample)
			}
		})
	}

	t.Run("404 returns ErrTransactionNotFound", func(t *testing.T) {
		srv := httptest.NewServer(h2c.NewHandler(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }),
			&http2.Server{},
		))
		t.Cleanup(srv.Close)

		c := NewHTTP2(srv.URL, serde.JSON)
		_, err := c.GetByID(context.Background(), "missing")
		if !errors.Is(err, app.ErrTransactionNotFound) {
			t.Errorf("err = %v, want ErrTransactionNotFound", err)
		}
	})

	t.Run("5xx returns wrapped error, not the sentinel", func(t *testing.T) {
		srv := httptest.NewServer(h2c.NewHandler(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			}),
			&http2.Server{},
		))
		t.Cleanup(srv.Close)

		c := NewHTTP2(srv.URL, serde.Avro)
		_, err := c.GetByID(context.Background(), "x")
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if errors.Is(err, app.ErrTransactionNotFound) {
			t.Error("5xx must not be mapped to ErrTransactionNotFound")
		}
	})

	t.Run("GetBatch round-trips a list over HTTP/2", func(t *testing.T) {
		want := []app.Transaction{sample, sample, sample}
		for name, codec := range codecs {
			t.Run(name, func(t *testing.T) {
				var gotPath string
				h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotPath = r.URL.Path
					body, _ := serde.ForAccept(r.Header.Get("Accept")).MarshalList(want)
					w.Header().Set("Content-Type", r.Header.Get("Accept"))
					_, _ = w.Write(body)
				})
				srv := httptest.NewServer(h2c.NewHandler(h, &http2.Server{}))
				t.Cleanup(srv.Close)

				c := NewHTTP2(srv.URL, codec)
				got, err := c.GetBatch(context.Background(), 3)
				if err != nil {
					t.Fatalf("GetBatch: %v", err)
				}
				if gotPath != "/v1/transactions/3" {
					t.Errorf("path = %q, want /v1/transactions/3", gotPath)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v\nwant %+v", got, want)
				}
			})
		}
	})
}
