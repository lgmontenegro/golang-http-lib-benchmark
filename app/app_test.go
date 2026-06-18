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
	tx    Transaction
	batch []Transaction
	err   error
}

func (f fakeTxRepo) GetByID(_ context.Context, _ string) (Transaction, error) {
	return f.tx, f.err
}

func (f fakeTxRepo) GetBatch(_ context.Context, _ int) ([]Transaction, error) {
	return f.batch, f.err
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

func TestGetTransactions(t *testing.T) {
	batch := []Transaction{
		{ID: "a", Value: 1, CreateDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "b", Value: 2, CreateDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}

	t.Run("200 returns the batch as a JSON array", func(t *testing.T) {
		a := New(fakeTxRepo{batch: batch})
		resp := a.GetTransactions(context.Background(), server.Request{
			Params: map[string]string{"count": "2"},
		})
		if resp.Status != 200 {
			t.Fatalf("status = %d, want 200", resp.Status)
		}
		var got []Transaction
		if err := json.Unmarshal(resp.Body, &got); err != nil {
			t.Fatalf("unmarshal body %s: %v", resp.Body, err)
		}
		if !reflect.DeepEqual(got, batch) {
			t.Errorf("body = %+v\nwant     = %+v", got, batch)
		}
	})

	t.Run("bad count returns 400", func(t *testing.T) {
		a := New(fakeTxRepo{batch: batch})
		for _, count := range []string{"", "0", "-1", "abc", "999999"} {
			resp := a.GetTransactions(context.Background(), server.Request{
				Params: map[string]string{"count": count},
			})
			if resp.Status != 400 {
				t.Errorf("count=%q: status = %d, want 400", count, resp.Status)
			}
		}
	})

	t.Run("repo error returns 500", func(t *testing.T) {
		a := New(fakeTxRepo{err: errors.New("boom")})
		resp := a.GetTransactions(context.Background(), server.Request{
			Params: map[string]string{"count": "5"},
		})
		if resp.Status != 500 {
			t.Errorf("status = %d, want 500", resp.Status)
		}
	})
}
