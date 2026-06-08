// Package servertest provides a shared HTTP integration suite that any
// implementation of server.Server can run to verify it correctly handles
// route registration, path parameters, response shape, and Start/Shutdown
// lifecycle. It lives under internal/ so only packages rooted at
// example.com/httpdi/server can import it.
package servertest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"example.com/httpdi/server"
)

// RunSuite registers test routes on s, starts it on addr, exercises it
// over real HTTP, then shuts it down. addr should be a free port unique
// per adapter (e.g. ":18081") to avoid conflicts when adapter test
// packages run in parallel.
func RunSuite(t *testing.T, s server.Server, addr string) {
	t.Helper()

	s.RegisterRoute("GET", "/health", func(_ context.Context, _ server.Request) server.Response {
		return server.Response{
			Status:  200,
			Body:    []byte(`{"status":"ok"}`),
			Headers: map[string]string{"Content-Type": "application/json"},
		}
	})
	s.RegisterRoute("GET", "/hello/:name", func(_ context.Context, req server.Request) server.Response {
		body := fmt.Sprintf(`{"hello":%q}`, req.Params["name"])
		return server.Response{
			Status:  200,
			Body:    []byte(body),
			Headers: map[string]string{"Content-Type": "application/json"},
		}
	})

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start(addr) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	base := "http://localhost" + addr
	waitReady(t, base+"/health", errCh, 2*time.Second)

	t.Run("health returns ok JSON", func(t *testing.T) {
		body, status := mustGet(t, base+"/health")
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		var got map[string]string
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["status"] != "ok" {
			t.Errorf("status = %q, want ok", got["status"])
		}
	})

	t.Run("hello echoes path param", func(t *testing.T) {
		body, status := mustGet(t, base+"/hello/leonardo")
		if status != 200 {
			t.Fatalf("status = %d, want 200", status)
		}
		var got map[string]string
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["hello"] != "leonardo" {
			t.Errorf("hello = %q, want leonardo", got["hello"])
		}
	})

	t.Run("unknown route is 404", func(t *testing.T) {
		_, status := mustGet(t, base+"/no-such-route")
		if status != 404 {
			t.Errorf("status = %d, want 404", status)
		}
	})
}

// waitReady polls health until it responds or the deadline is hit. If the
// server goroutine returns an error before becoming reachable, surface it
// instead of timing out on a misleading "not ready" message.
func waitReady(t *testing.T, healthURL string, errCh <-chan error, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("server start: %v", err)
		default:
		}
		resp, err := http.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server not ready within %s", timeout)
}

func mustGet(t *testing.T, url string) ([]byte, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body, resp.StatusCode
}
