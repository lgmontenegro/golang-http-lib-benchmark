package serde

import (
	"fmt"
	"time"

	"github.com/hamba/avro/v2"
	"google.golang.org/grpc/encoding"

	"example.com/httpdi/app"
)

// AvroCodecName is the gRPC content-subtype registered for Avro. Clients
// select it per-call with grpc.CallContentSubtype(AvroCodecName).
const AvroCodecName = "avro"

// Avro wire schemas. Dates use the timestamp-micros logical type, which the
// hamba library maps to time.Time. Field names match the JSON tags on the
// domain types so the wire stays self-describing.
var (
	avroTxSchema = avro.MustParse(`{
		"type": "record",
		"name": "Transaction",
		"namespace": "httpdi.transactions.v1",
		"fields": [
			{"name": "id", "type": "string"},
			{"name": "value", "type": "double"},
			{"name": "create_date", "type": {"type": "long", "logicalType": "timestamp-micros"}},
			{"name": "customer", "type": {
				"type": "record", "name": "Customer", "fields": [
					{"name": "id", "type": "string"},
					{"name": "nome", "type": "string"},
					{"name": "create_date", "type": {"type": "long", "logicalType": "timestamp-micros"}}
				]}},
			{"name": "cart_snapshot", "type": {
				"type": "record", "name": "CartSnapshot", "fields": [
					{"name": "id", "type": "string"},
					{"name": "create_date", "type": {"type": "long", "logicalType": "timestamp-micros"}}
				]}}
		]
	}`)

	avroReqSchema = avro.MustParse(`{
		"type": "record",
		"name": "GetByIdRequest",
		"namespace": "httpdi.transactions.v1",
		"fields": [{"name": "id", "type": "string"}]
	}`)

	// avroListSchema is an Avro array of the Transaction record — the batch
	// wire shape. Built from avroTxSchema so the record is defined once.
	avroListSchema = avro.NewArraySchema(avroTxSchema)
)

// AvroTransaction is the Avro wire DTO mirroring app.Transaction. The avro:
// tags live here, never on the domain type.
type AvroTransaction struct {
	ID           string           `avro:"id"`
	Value        float64          `avro:"value"`
	CreateDate   time.Time        `avro:"create_date"`
	Customer     avroCustomer     `avro:"customer"`
	CartSnapshot avroCartSnapshot `avro:"cart_snapshot"`
}

type avroCustomer struct {
	ID         string    `avro:"id"`
	Nome       string    `avro:"nome"`
	CreateDate time.Time `avro:"create_date"`
}

type avroCartSnapshot struct {
	ID         string    `avro:"id"`
	CreateDate time.Time `avro:"create_date"`
}

// AvroGetByIdRequest is the Avro wire DTO for the gRPC-Avro GetById request.
type AvroGetByIdRequest struct {
	ID string `avro:"id"`
}

// AvroGetBatchRequest is the Avro wire DTO for the gRPC-Avro GetBatch request.
type AvroGetBatchRequest struct {
	Limit int32 `avro:"limit"`
}

var avroBatchReqSchema = avro.MustParse(`{
	"type": "record",
	"name": "GetBatchRequest",
	"namespace": "httpdi.transactions.v1",
	"fields": [{"name": "limit", "type": "int"}]
}`)

// DomainToAvro maps the domain aggregate onto its Avro DTO. Shared by the
// REST Avro codec and the gRPC-Avro dataservice handler.
func DomainToAvro(tx app.Transaction) AvroTransaction {
	return AvroTransaction{
		ID:         tx.ID,
		Value:      tx.Value,
		CreateDate: tx.CreateDate,
		Customer: avroCustomer{
			ID:         tx.Customer.ID,
			Nome:       tx.Customer.Nome,
			CreateDate: tx.Customer.CreateDate,
		},
		CartSnapshot: avroCartSnapshot{
			ID:         tx.CartSnapshot.ID,
			CreateDate: tx.CartSnapshot.CreateDate,
		},
	}
}

// AvroToDomain maps an Avro DTO back to the domain aggregate. Shared by the
// REST Avro codec and the gRPC-Avro client adapter.
func AvroToDomain(a AvroTransaction) app.Transaction {
	return app.Transaction{
		ID:         a.ID,
		Value:      a.Value,
		CreateDate: a.CreateDate,
		Customer: app.Customer{
			ID:         a.Customer.ID,
			Nome:       a.Customer.Nome,
			CreateDate: a.Customer.CreateDate,
		},
		CartSnapshot: app.CartSnapshot{
			ID:         a.CartSnapshot.ID,
			CreateDate: a.CartSnapshot.CreateDate,
		},
	}
}

// avroCodec is the Avro implementation of the REST Codec.
type avroCodec struct{}

func (avroCodec) ContentType() string { return ContentAvro }

func (avroCodec) Marshal(tx app.Transaction) ([]byte, error) {
	return avro.Marshal(avroTxSchema, DomainToAvro(tx))
}

func (avroCodec) Unmarshal(data []byte) (app.Transaction, error) {
	var dto AvroTransaction
	if err := avro.Unmarshal(avroTxSchema, data, &dto); err != nil {
		return app.Transaction{}, err
	}
	return AvroToDomain(dto), nil
}

func (avroCodec) MarshalList(txs []app.Transaction) ([]byte, error) {
	dtos := make([]AvroTransaction, len(txs))
	for i, tx := range txs {
		dtos[i] = DomainToAvro(tx)
	}
	return avro.Marshal(avroListSchema, dtos)
}

func (avroCodec) UnmarshalList(data []byte) ([]app.Transaction, error) {
	var dtos []AvroTransaction
	if err := avro.Unmarshal(avroListSchema, data, &dtos); err != nil {
		return nil, err
	}
	txs := make([]app.Transaction, len(dtos))
	for i, dto := range dtos {
		txs[i] = AvroToDomain(dto)
	}
	return txs, nil
}

// avroGRPCCodec implements grpc/encoding.Codec so the gRPC-Avro path carries
// Avro DTOs over the wire instead of protobuf. It selects the schema by the
// concrete message type — the gRPC-Avro service only ever exchanges these two.
type avroGRPCCodec struct{}

func init() { encoding.RegisterCodec(avroGRPCCodec{}) }

func (avroGRPCCodec) Name() string { return AvroCodecName }

func (avroGRPCCodec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case *AvroTransaction:
		return avro.Marshal(avroTxSchema, m)
	case *[]AvroTransaction:
		return avro.Marshal(avroListSchema, *m)
	case *AvroGetByIdRequest:
		return avro.Marshal(avroReqSchema, m)
	case *AvroGetBatchRequest:
		return avro.Marshal(avroBatchReqSchema, m)
	default:
		return nil, fmt.Errorf("serde: avro grpc codec cannot marshal %T", v)
	}
}

func (avroGRPCCodec) Unmarshal(data []byte, v any) error {
	switch m := v.(type) {
	case *AvroTransaction:
		return avro.Unmarshal(avroTxSchema, data, m)
	case *[]AvroTransaction:
		return avro.Unmarshal(avroListSchema, data, m)
	case *AvroGetByIdRequest:
		return avro.Unmarshal(avroReqSchema, data, m)
	case *AvroGetBatchRequest:
		return avro.Unmarshal(avroBatchReqSchema, data, m)
	default:
		return fmt.Errorf("serde: avro grpc codec cannot unmarshal %T", v)
	}
}
