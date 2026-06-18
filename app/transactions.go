package app

import (
	"context"
	"errors"
	"time"
)

// Customer is the buyer of a transaction.
type Customer struct {
	ID         string    `json:"id"`
	Nome       string    `json:"nome"`
	CreateDate time.Time `json:"create_date"`
}

// CartSnapshot captures the cart state at checkout time. Modelled 1:1 with
// Transaction in the current schema.
type CartSnapshot struct {
	ID         string    `json:"id"`
	CreateDate time.Time `json:"create_date"`
}

// Transaction is the denormalised aggregate the API returns: the transaction
// row plus the customer and cart_snapshot it points at. Adapters are
// responsible for assembling this from whatever storage shape they use.
type Transaction struct {
	ID           string       `json:"id"`
	Value        float64      `json:"value"`
	CreateDate   time.Time    `json:"create_date"`
	Customer     Customer     `json:"customer"`
	CartSnapshot CartSnapshot `json:"cart_snapshot"`
}

// TransactionRepository is the driven port for transaction storage.
// Adapter implementations live outside the app package.
type TransactionRepository interface {
	GetByID(ctx context.Context, id string) (Transaction, error)
	// GetBatch returns up to limit aggregates. Used by the /v1/transactions
	// list endpoint to drive the wire codecs under larger payloads.
	GetBatch(ctx context.Context, limit int) ([]Transaction, error)
}

// ErrTransactionNotFound is returned by TransactionRepository implementations
// when the requested id has no row. App handlers translate it to 404.
var ErrTransactionNotFound = errors.New("transaction not found")
