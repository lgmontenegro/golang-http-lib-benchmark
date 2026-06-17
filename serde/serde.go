// Package serde holds the transaction (de)serialization codecs used by the
// network adapters. It keeps wire-format coupling — JSON, protobuf, Avro —
// out of the domain: app.Transaction carries only json: tags, and every
// other shape (protobuf messages, Avro DTOs) lives behind a Codec here.
//
// The same codecs back two transports: the REST/HTTP adapters negotiate one
// via the Accept/Content-Type header (see ForAccept), and the gRPC-Avro path
// registers a grpc/encoding.Codec (see avro.go) so the front can swap
// serialization without the application core ever knowing.
package serde

import "example.com/httpdi/app"

// Content-Type / Accept values the REST codecs negotiate on.
const (
	ContentJSON     = "application/json"
	ContentProtobuf = "application/x-protobuf"
	ContentAvro     = "application/avro"
)

// Codec marshals an app.Transaction to bytes and back for a single wire
// format. Implementations are stateless and safe for concurrent use.
type Codec interface {
	// ContentType is the MIME type carried in the REST Accept/Content-Type
	// header so client and server agree on the format.
	ContentType() string
	// Marshal encodes the aggregate to its wire bytes.
	Marshal(tx app.Transaction) ([]byte, error)
	// Unmarshal decodes wire bytes back into the aggregate.
	Unmarshal(data []byte) (app.Transaction, error)
}

// The three REST codecs. Stateless singletons — share them freely.
var (
	JSON     Codec = jsonCodec{}
	Protobuf Codec = pbCodec{}
	Avro     Codec = avroCodec{}
)

// ForAccept selects a Codec from a REST Accept header value. Unknown or
// empty values fall back to JSON, so a plain `curl` still gets a readable
// response.
func ForAccept(accept string) Codec {
	switch accept {
	case ContentProtobuf:
		return Protobuf
	case ContentAvro:
		return Avro
	default:
		return JSON
	}
}
