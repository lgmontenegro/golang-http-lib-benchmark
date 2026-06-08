package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"example.com/httpdi/server"
)

// fakeTxRepo is a controllable TransactionRepository for handler tests.
type fakeTxRepo struct {
	tx  Transaction
	err error
}

func (f fakeTxRepo) GetByID(_ context.Context, _ string) (Transaction, error) {
	return f.tx, f.err
}

func TestHealth(t *testing.T) {
	a := New(fakeTxRepo{})
	resp := a.Health(context.Background(), server.Request{})

	if resp.Status != 200 {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if got := resp.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body map[string]string
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf(`status = %q, want "ok"`, body["status"])
	}
}

func TestHello(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]string
		want   string
	}{
		{"with name", map[string]string{"name": "leonardo"}, "leonardo"},
		{"empty name defaults to world", map[string]string{"name": ""}, "world"},
		{"missing name key defaults to world", map[string]string{}, "world"},
		{"nil params defaults to world", nil, "world"},
	}

	a := New(fakeTxRepo{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := a.Hello(context.Background(), server.Request{Params: tt.params})
			if resp.Status != 200 {
				t.Errorf("status = %d, want 200", resp.Status)
			}
			var body map[string]string
			if err := json.Unmarshal(resp.Body, &body); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}
			if body["hello"] != tt.want {
				t.Errorf("hello = %q, want %q", body["hello"], tt.want)
			}
		})
	}
}

func TestGetTransactionFound(t *testing.T) {
	sample := Transaction{
		ID:         "abc",
		Value:      100.50,
		CreateDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Customer: Customer{
			ID:         "cust-1",
			Nome:       "Leonardo",
			CreateDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		CartSnapshot: CartSnapshot{
			ID:         "cart-1",
			CreateDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	}

	a := New(fakeTxRepo{tx: sample})
	resp := a.GetTransaction(context.Background(), server.Request{
		Params: map[string]string{"transaction_id": "abc"},
	})

	if resp.Status != 200 {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if got := resp.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	// Round-trip through JSON to confirm the body matches the domain object
	// (catches missing fields, renames, and broken nesting in one assert).
	var got Transaction
	if err := json.Unmarshal(resp.Body, &got); err != nil {
		t.Fatalf("unmarshal body %s: %v", resp.Body, err)
	}
	if !reflect.DeepEqual(got, sample) {
		t.Errorf("body = %+v\nwant     = %+v", got, sample)
	}
}

func TestGetTransactionErrors(t *testing.T) {
	tests := []struct {
		name       string
		repoErr    error
		wantStatus int
		wantError  string
	}{
		{"not found returns 404", ErrTransactionNotFound, 404, "not found"},
		{"repo error returns 500", errors.New("boom"), 500, "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New(fakeTxRepo{err: tt.repoErr})
			resp := a.GetTransaction(context.Background(), server.Request{
				Params: map[string]string{"transaction_id": "abc"},
			})
			if resp.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.Status, tt.wantStatus)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(resp.Body, &body); err != nil {
				t.Fatalf("unmarshal body %s: %v", resp.Body, err)
			}
			if body.Error != tt.wantError {
				t.Errorf("error = %q, want %q", body.Error, tt.wantError)
			}
		})
	}
}
