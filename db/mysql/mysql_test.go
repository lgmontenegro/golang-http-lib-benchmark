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

	t.Run("seeded id returns full aggregate", func(t *testing.T) {
		tx, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000001")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}

		if tx.ID != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("ID = %q", tx.ID)
		}
		// Seeded `value` is RAND() per row, so just check it scanned > 0.
		if tx.Value <= 0 {
			t.Errorf("Value = %v, want > 0", tx.Value)
		}
		if tx.CreateDate.IsZero() {
			t.Error("CreateDate is zero — sqlx didn't parse DATETIME (DSN missing parseTime=true?)")
		}

		if tx.Customer.ID != "11111111-1111-1111-1111-000000000001" {
			t.Errorf("Customer.ID = %q", tx.Customer.ID)
		}
		if tx.Customer.Nome != "Customer #1" {
			t.Errorf("Customer.Nome = %q, want %q", tx.Customer.Nome, "Customer #1")
		}
		if tx.Customer.CreateDate.IsZero() {
			t.Error("Customer.CreateDate is zero")
		}

		if tx.CartSnapshot.ID != "22222222-2222-2222-2222-000000000001" {
			t.Errorf("CartSnapshot.ID = %q", tx.CartSnapshot.ID)
		}
		if tx.CartSnapshot.CreateDate.IsZero() {
			t.Error("CartSnapshot.CreateDate is zero")
		}
	})

	t.Run("unknown id returns ErrTransactionNotFound", func(t *testing.T) {
		_, err := repo.GetByID(context.Background(), "does-not-exist")
		if !errors.Is(err, app.ErrTransactionNotFound) {
			t.Errorf("err = %v, want ErrTransactionNotFound", err)
		}
	})
}
