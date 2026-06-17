// Package restclient implements app.TransactionRepository by calling the
// dataservice over HTTP. The wire format is pluggable via serde.Codec
// (JSON, protobuf, Avro) and the transport is either HTTP/1.1 (New) or
// HTTP/2 cleartext (NewHTTP2) — so the same adapter backs every REST cell
// of the benchmark matrix. Drop-in replacement for db/mysql when the front
// runs as a gateway in front of a separate data tier.
package restclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http2"

	"example.com/httpdi/app"
	"example.com/httpdi/serde"
)

// TransactionClient hits the dataservice's GET /v1/transaction/{id} endpoint
// and decodes the aggregate using its configured codec.
type TransactionClient struct {
	base   string
	client *http.Client
	codec  serde.Codec
}

// New constructs an HTTP/1.1 client using JSON. baseURL is the dataservice
// root (e.g. "http://localhost:9090"); a trailing slash is tolerated.
func New(baseURL string) *TransactionClient {
	return &TransactionClient{
		base:   strings.TrimRight(baseURL, "/"),
		client: &http.Client{Timeout: 5 * time.Second},
		codec:  serde.JSON,
	}
}

// NewHTTP2 constructs a client that talks HTTP/2 cleartext (h2c) with the
// dataservice using the given codec. Same semantics as New, but forces the
// HTTP/2 transport so JSON/protobuf/Avro can be compared over HTTP/2 against
// gRPC isolating only protocol and serialization.
func NewHTTP2(baseURL string, codec serde.Codec) *TransactionClient {
	tr := &http2.Transport{
		AllowHTTP: true,
		// h2c: dial a plain TCP connection instead of TLS. The tls.Config
		// is ignored because the scheme stays http://.
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	return &TransactionClient{
		base:   strings.TrimRight(baseURL, "/"),
		client: &http.Client{Transport: tr, Timeout: 5 * time.Second},
		codec:  codec,
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
	req.Header.Set("Accept", c.codec.ContentType())
	resp, err := c.client.Do(req)
	if err != nil {
		return app.Transaction{}, fmt.Errorf("rest call: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return app.Transaction{}, fmt.Errorf("rest read body: %w", err)
		}
		tx, err := c.codec.Unmarshal(body)
		if err != nil {
			return app.Transaction{}, fmt.Errorf("rest decode: %w", err)
		}
		return tx, nil
	case http.StatusNotFound:
		return app.Transaction{}, app.ErrTransactionNotFound
	default:
		return app.Transaction{}, fmt.Errorf("rest status %d", resp.StatusCode)
	}
}
