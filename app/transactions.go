package app

import (
	"context"
	"errors"
	"time"
)

// Transaction is the financial transaction the system reads. Struct tags
// describe both the SQL column mapping (consumed by sqlx in the mysql
// adapter) and the JSON shape returned by the HTTP layer.
type Transaction struct {
	ID        string    `db:"id" json:"id"`
	Amount    float64   `db:"amount" json:"amount"`
	Currency  string    `db:"currency" json:"currency"`
	Status    string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// TransactionRepository is the driven port for transaction storage.
// Adapter implementations live outside the app package.
type TransactionRepository interface {
	GetByID(ctx context.Context, id string) (Transaction, error)
}

// ErrTransactionNotFound is returned by TransactionRepository implementations
// when the requested id has no row. App handlers translate it to 404.
var ErrTransactionNotFound = errors.New("transaction not found")
