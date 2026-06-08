// Package mysql implements app.TransactionRepository on top of MySQL via
// sqlx and the go-sql-driver/mysql driver.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"example.com/httpdi/app"
)

// TransactionRepo adapts MySQL to app.TransactionRepository.
type TransactionRepo struct {
	db *sqlx.DB
}

// New opens a MySQL connection pool with conservative defaults tuned for a
// benchmark workload (one read per request, high concurrency).
func New(dsn string) (*TransactionRepo, error) {
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql connect: %w", err)
	}
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(50)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &TransactionRepo{db: db}, nil
}

// GetByID returns the transaction row identified by id, or
// app.ErrTransactionNotFound if no row matches.
func (r *TransactionRepo) GetByID(ctx context.Context, id string) (app.Transaction, error) {
	var tx app.Transaction
	err := r.db.GetContext(ctx, &tx,
		"SELECT id, amount, currency, status, created_at FROM transactions WHERE id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Transaction{}, app.ErrTransactionNotFound
	}
	if err != nil {
		return app.Transaction{}, fmt.Errorf("get transaction %s: %w", id, err)
	}
	return tx, nil
}

// Close releases the underlying connection pool.
func (r *TransactionRepo) Close() error {
	return r.db.Close()
}
