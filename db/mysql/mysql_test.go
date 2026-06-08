//go:build integration

// Integration tests for the MySQL adapter. Requires `docker compose up -d`
// (or some MySQL with the schema and seed from mysql-init/) reachable on
// 127.0.0.1:3306. Run with: `go test -tags integration ./db/mysql`.

package mysql

import (
	"context"
	"errors"
	"os"
	"testing"

	"example.com/httpdi/app"
)

const fallbackDSN = "httpdi:httpdipass@tcp(127.0.0.1:3306)/httpdi?parseTime=true"

func newRepo(t *testing.T) *TransactionRepo {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = fallbackDSN
	}
	repo, err := New(dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", dsn, err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestGetByID(t *testing.T) {
	repo := newRepo(t)

	t.Run("seeded id returns full row", func(t *testing.T) {
		tx, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000001")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if tx.ID != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("ID = %q", tx.ID)
		}
		if tx.Amount != 100.00 {
			t.Errorf("Amount = %v, want 100.00", tx.Amount)
		}
		if tx.Currency != "USD" {
			t.Errorf("Currency = %q, want USD", tx.Currency)
		}
		if tx.Status != "completed" {
			t.Errorf("Status = %q, want completed", tx.Status)
		}
		if tx.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero — sqlx didn't parse DATETIME (DSN missing parseTime=true?)")
		}
	})

	t.Run("unknown id returns ErrTransactionNotFound", func(t *testing.T) {
		_, err := repo.GetByID(context.Background(), "does-not-exist")
		if !errors.Is(err, app.ErrTransactionNotFound) {
			t.Errorf("err = %v, want ErrTransactionNotFound", err)
		}
	})
}
