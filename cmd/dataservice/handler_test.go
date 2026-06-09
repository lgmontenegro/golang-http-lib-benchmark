package main

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

type fakeTxRepo struct {
	tx  app.Transaction
	err error
}

func (f fakeTxRepo) GetByID(_ context.Context, _ string) (app.Transaction, error) {
	return f.tx, f.err
}

func TestGetTransactionHandlerFound(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/transaction/{id}", makeGetTransactionHandler(fakeTxRepo{tx: sample}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/transaction/abc", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var got app.Transaction
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if !reflect.DeepEqual(got, sample) {
		t.Errorf("body = %+v\nwant     = %+v", got, sample)
	}
}

func TestGetTransactionHandlerErrors(t *testing.T) {
	tests := []struct {
		name       string
		repoErr    error
		wantStatus int
		wantError  string
	}{
		{"not found returns 404", app.ErrTransactionNotFound, 404, "not found"},
		{"repo error returns 500", errors.New("boom"), 500, "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/transaction/{id}", makeGetTransactionHandler(fakeTxRepo{err: tt.repoErr}))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/transaction/abc", nil)
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal body %s: %v", rec.Body.String(), err)
			}
			if body.Error != tt.wantError {
				t.Errorf("error = %q, want %q", body.Error, tt.wantError)
			}
		})
	}
}
