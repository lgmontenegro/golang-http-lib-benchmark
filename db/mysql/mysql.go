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

// transactionRow is the flat row produced by the JOIN, with column aliases
// matching the SELECT below. It exists so the domain types in app/ stay
// free of `db:` tags and infrastructure concerns.
type transactionRow struct {
	TransactionID          string    `db:"transaction_id"`
	TransactionValue       float64   `db:"transaction_value"`
	TransactionCreateDate  time.Time `db:"transaction_create_date"`
	CustomerID             string    `db:"customer_id"`
	CustomerNome           string    `db:"customer_nome"`
	CustomerCreateDate     time.Time `db:"customer_create_date"`
	CartSnapshotID         string    `db:"cart_snapshot_id"`
	CartSnapshotCreateDate time.Time `db:"cart_snapshot_create_date"`
}

func (r transactionRow) toDomain() app.Transaction {
	return app.Transaction{
		ID:         r.TransactionID,
		Value:      r.TransactionValue,
		CreateDate: r.TransactionCreateDate,
		Customer: app.Customer{
			ID:         r.CustomerID,
			Nome:       r.CustomerNome,
			CreateDate: r.CustomerCreateDate,
		},
		CartSnapshot: app.CartSnapshot{
			ID:         r.CartSnapshotID,
			CreateDate: r.CartSnapshotCreateDate,
		},
	}
}

const getTransactionByIDQuery = `
SELECT
    t.id           AS transaction_id,
    t.value        AS transaction_value,
    t.create_date  AS transaction_create_date,
    c.id           AS customer_id,
    c.nome         AS customer_nome,
    c.create_date  AS customer_create_date,
    cs.id          AS cart_snapshot_id,
    cs.create_date AS cart_snapshot_create_date
FROM ` + "`transaction`" + ` t
JOIN customer       c  ON c.id  = t.customer_id
JOIN cart_snapshot  cs ON cs.transaction_id = t.id
WHERE t.id = ?
`

// GetByID joins transaction → customer + cart_snapshot and returns the
// denormalised aggregate. Returns app.ErrTransactionNotFound if no row
// matches. A transaction without a matching cart_snapshot would also
// surface as ErrTransactionNotFound because of the INNER JOIN — that's
// intentional: the schema constrains the relationship to 1:1.
func (r *TransactionRepo) GetByID(ctx context.Context, id string) (app.Transaction, error) {
	var row transactionRow
	err := r.db.GetContext(ctx, &row, getTransactionByIDQuery, id)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Transaction{}, app.ErrTransactionNotFound
	}
	if err != nil {
		return app.Transaction{}, fmt.Errorf("get transaction %s: %w", id, err)
	}
	return row.toDomain(), nil
}

const getTransactionBatchQuery = `
SELECT
    t.id           AS transaction_id,
    t.value        AS transaction_value,
    t.create_date  AS transaction_create_date,
    c.id           AS customer_id,
    c.nome         AS customer_nome,
    c.create_date  AS customer_create_date,
    cs.id          AS cart_snapshot_id,
    cs.create_date AS cart_snapshot_create_date
FROM ` + "`transaction`" + ` t
JOIN customer       c  ON c.id  = t.customer_id
JOIN cart_snapshot  cs ON cs.transaction_id = t.id
LIMIT ?
`

// GetBatch returns up to limit aggregates — the first N seeded transactions.
// Same JOIN as GetByID; exists to drive the wire codecs under larger payloads.
//
// No ORDER BY on purpose: an explicit `ORDER BY t.id` makes MySQL materialise
// the whole join into a temp table and filesort it on *every* request
// (~135 ms here), which swamps the serialization signal the benchmark is after.
// Without it the optimiser drives from cart_snapshot's transaction_id index and
// stops at LIMIT, so rows still come back in ascending id order (the first N)
// but in ~1 ms. An empty result is returned as an empty slice, not an error.
func (r *TransactionRepo) GetBatch(ctx context.Context, limit int) ([]app.Transaction, error) {
	var rows []transactionRow
	if err := r.db.SelectContext(ctx, &rows, getTransactionBatchQuery, limit); err != nil {
		return nil, fmt.Errorf("get transaction batch (limit=%d): %w", limit, err)
	}
	txs := make([]app.Transaction, len(rows))
	for i, row := range rows {
		txs[i] = row.toDomain()
	}
	return txs, nil
}

// Close releases the underlying connection pool.
func (r *TransactionRepo) Close() error {
	return r.db.Close()
}
