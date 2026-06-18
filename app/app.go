// Package app holds the application core. Inbound HTTP requests are
// translated by the server adapter into server.Request and dispatched to
// App methods. Outbound dependencies (DB, queues, ...) live here as
// interface fields on App and are implemented by adapters in other packages.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"example.com/httpdi/server"
)

// maxBatch caps the list endpoint so a bad ":count" can't ask for the whole
// table. Comfortably above the largest benchmark size (5000).
const maxBatch = 10000

// App is the application core. Driven ports are injected as interface
// fields so the core never depends on concrete adapters.
type App struct {
	transactions TransactionRepository
}

// New constructs an App with its dependencies wired in.
func New(transactions TransactionRepository) *App {
	return &App{transactions: transactions}
}

// Health responds with a static JSON readiness marker.
func (a *App) Health(_ context.Context, _ server.Request) server.Response {
	return jsonResponse(200, []byte(`{"status":"ok"}`))
}

// Hello greets the ":name" path parameter, defaulting to "world".
func (a *App) Hello(_ context.Context, req server.Request) server.Response {
	name := req.Params["name"]
	if name == "" {
		name = "world"
	}
	return jsonResponse(200, []byte(fmt.Sprintf(`{"hello":%q}`, name)))
}

// GetTransaction reads :transaction_id from the request, fetches it via the
// repository, and returns the row as JSON. Missing → 404, repo error → 500.
func (a *App) GetTransaction(ctx context.Context, req server.Request) server.Response {
	id := req.Params["transaction_id"]
	tx, err := a.transactions.GetByID(ctx, id)
	if errors.Is(err, ErrTransactionNotFound) {
		return jsonResponse(404, []byte(`{"error":"not found"}`))
	}
	if err != nil {
		log.Printf("app: get transaction %s: %v", id, err)
		return jsonResponse(500, []byte(`{"error":"internal"}`))
	}
	body, err := json.Marshal(tx)
	if err != nil {
		log.Printf("app: marshal transaction %s: %v", id, err)
		return jsonResponse(500, []byte(`{"error":"internal"}`))
	}
	return jsonResponse(200, body)
}

// GetTransactions reads :count from the request, fetches that many aggregates
// via the repository, and returns them as a JSON array. Bad count → 400,
// repo error → 500. The external response is always JSON; the wire format
// under test is the front→dataservice hop chosen by -repo.
func (a *App) GetTransactions(ctx context.Context, req server.Request) server.Response {
	count, err := strconv.Atoi(req.Params["count"])
	if err != nil || count < 1 || count > maxBatch {
		return jsonResponse(400, []byte(`{"error":"invalid count"}`))
	}
	txs, err := a.transactions.GetBatch(ctx, count)
	if err != nil {
		log.Printf("app: get transactions (count=%d): %v", count, err)
		return jsonResponse(500, []byte(`{"error":"internal"}`))
	}
	body, err := json.Marshal(txs)
	if err != nil {
		log.Printf("app: marshal transactions (count=%d): %v", count, err)
		return jsonResponse(500, []byte(`{"error":"internal"}`))
	}
	return jsonResponse(200, body)
}

func jsonResponse(status int, body []byte) server.Response {
	return server.Response{
		Status:  status,
		Body:    body,
		Headers: map[string]string{"Content-Type": "application/json"},
	}
}
