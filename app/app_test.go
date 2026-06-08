package app

import (
	"context"
	"encoding/json"
	"errors"
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

func TestGetTransaction(t *testing.T) {
	sample := Transaction{
		ID:        "abc",
		Amount:    100.50,
		Currency:  "USD",
		Status:    "completed",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	tests := []struct {
		name       string
		repo       fakeTxRepo
		wantStatus int
		wantBody   map[string]any
	}{
		{
			name:       "found returns 200 with row",
			repo:       fakeTxRepo{tx: sample},
			wantStatus: 200,
			wantBody: map[string]any{
				"id":         "abc",
				"amount":     100.50,
				"currency":   "USD",
				"status":     "completed",
				"created_at": "2026-01-02T03:04:05Z",
			},
		},
		{
			name:       "not found returns 404",
			repo:       fakeTxRepo{err: ErrTransactionNotFound},
			wantStatus: 404,
			wantBody:   map[string]any{"error": "not found"},
		},
		{
			name:       "repo error returns 500",
			repo:       fakeTxRepo{err: errors.New("boom")},
			wantStatus: 500,
			wantBody:   map[string]any{"error": "internal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New(tt.repo)
			resp := a.GetTransaction(context.Background(), server.Request{
				Params: map[string]string{"transaction_id": "abc"},
			})
			if resp.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.Status, tt.wantStatus)
			}
			if got := resp.Headers["Content-Type"]; got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}

			var body map[string]any
			if err := json.Unmarshal(resp.Body, &body); err != nil {
				t.Fatalf("unmarshal body %q: %v", resp.Body, err)
			}
			for k, want := range tt.wantBody {
				if got := body[k]; got != want {
					t.Errorf("body[%q] = %v, want %v", k, got, want)
				}
			}
		})
	}
}
