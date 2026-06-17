package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"example.com/httpdi/app"
	"example.com/httpdi/serde"
)

// healthHandler answers a static readiness probe. Used by bench-full.sh
// to wait until the dataservice is reachable before launching the front.
// Intentionally does not touch the DB — the probe should not race with
// connection-pool warmup.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// makeGetTransactionHandler returns an http.HandlerFunc that fetches an
// aggregate by id from the given repository. The success body is encoded in
// the format the caller asks for via the Accept header (JSON, protobuf, or
// Avro — see serde.ForAccept), so the same endpoint serves every REST
// serialization backend. 200 on success, 404 if missing (sentinel mapped to
// status), 500 otherwise. Error bodies stay JSON. The repo is
// constructor-injected so tests can drive it with a fake.
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

		codec := serde.ForAccept(r.Header.Get("Accept"))
		body, err := codec.Marshal(tx)
		if err != nil {
			log.Printf("dataservice: marshal %s: %v", id, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		w.Header().Set("Content-Type", codec.ContentType())
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			log.Printf("dataservice: write response %s: %v", id, err)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("dataservice: encode response: %v", err)
	}
}
