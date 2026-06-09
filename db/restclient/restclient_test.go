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

	"example.com/httpdi/app"
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
