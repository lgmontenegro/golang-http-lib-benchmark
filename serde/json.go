package serde

import (
	"encoding/json"

	"example.com/httpdi/app"
)

// jsonCodec is the stdlib encoding/json implementation of Codec. It is the
// default and the only human-readable wire format.
type jsonCodec struct{}

func (jsonCodec) ContentType() string { return ContentJSON }

func (jsonCodec) Marshal(tx app.Transaction) ([]byte, error) {
	return json.Marshal(tx)
}

func (jsonCodec) Unmarshal(data []byte) (app.Transaction, error) {
	var tx app.Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return app.Transaction{}, err
	}
	return tx, nil
}
