package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"example.com/httpdi/app"
)

// healthHandler answers a static readiness probe. Used by bench-full.sh
// to wait until the dataservice is reachable before launching the front.
// Intentionally does not touch the DB — the probe should not race with
// connection-pool warmup.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// makeGetTransactionHandler returns an http.HandlerFunc that fetches an
// aggregate by id from the given repository. 200 with the JSON aggregate
// on success, 404 if missing (sentinel mapped to status), 500 otherwise.
// The repo is constructor-injected so tests can drive it with a fake.
func makeGetTransactionHandler(repo app.TransactionRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		tx, err := repo.GetByID(r.Context(), id)
		if errors.Is(err, app.ErrTransactionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			log.Printf("dataservice: get %s: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		writeJSON(w, http.StatusOK, tx)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("dataservice: encode response: %v", err)
	}
}
