// Package restclient implements app.TransactionRepository by calling the
// dataservice over HTTP+JSON. Drop-in replacement for db/mysql when the
// front runs as a gateway in front of a separate data tier.
package restclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"example.com/httpdi/app"
)

// TransactionClient hits the dataservice's GET /v1/transaction/{id} endpoint
// and decodes the JSON aggregate into app.Transaction.
type TransactionClient struct {
	base   string
	client *http.Client
}

// New constructs a TransactionClient. baseURL is the dataservice root
// (e.g. "http://localhost:9090"); a trailing slash is tolerated.
func New(baseURL string) *TransactionClient {
	return &TransactionClient{
		base: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// GetByID fetches an aggregate from the upstream dataservice. HTTP 404 is
// mapped to app.ErrTransactionNotFound so callers can branch on the
// sentinel just as they do with the in-process MySQL adapter.
func (c *TransactionClient) GetByID(ctx context.Context, id string) (app.Transaction, error) {
	u := c.base + "/v1/transaction/" + url.PathEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return app.Transaction{}, fmt.Errorf("rest build request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return app.Transaction{}, fmt.Errorf("rest call: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var tx app.Transaction
		if err := json.NewDecoder(resp.Body).Decode(&tx); err != nil {
			return app.Transaction{}, fmt.Errorf("rest decode: %w", err)
		}
		return tx, nil
	case http.StatusNotFound:
		return app.Transaction{}, app.ErrTransactionNotFound
	default:
		return app.Transaction{}, fmt.Errorf("rest status %d", resp.StatusCode)
	}
}
