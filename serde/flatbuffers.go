package serde

import (
	"fmt"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"
	"google.golang.org/grpc/encoding"

	"example.com/httpdi/app"
	fb "example.com/httpdi/proto/transactionsfb"
)

// FlatCodecName is the gRPC content-subtype registered for FlatBuffers.
// Clients select it per-call with grpc.CallContentSubtype(FlatCodecName).
const FlatCodecName = "flatbuffers"

// flatCodec implements grpc/encoding.Codec for FlatBuffers. Following the
// FlatBuffers gRPC convention, the *outbound* message is an already-finished
// *flatbuffers.Builder and the *inbound* message is a generated table that
// gets Init'd over the received bytes — zero-copy on the read side.
type flatCodec struct{}

func init() { encoding.RegisterCodec(flatCodec{}) }

func (flatCodec) Name() string { return FlatCodecName }

func (flatCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*flatbuffers.Builder)
	if !ok {
		return nil, fmt.Errorf("serde: flat codec expects *flatbuffers.Builder, got %T", v)
	}
	return b.FinishedBytes(), nil
}

func (flatCodec) Unmarshal(data []byte, v any) error {
	msg, ok := v.(flatbuffers.FlatBuffer)
	if !ok {
		return fmt.Errorf("serde: flat codec expects FlatBuffer, got %T", v)
	}
	msg.Init(data, flatbuffers.GetUOffsetT(data))
	return nil
}

// ── builders (domain → FlatBuffers) ──────────────────────────────────────

// buildFlatTransaction writes a Transaction into b and returns its offset.
// Children (strings, nested tables) are built before the table is opened, as
// FlatBuffers requires.
func buildFlatTransaction(b *flatbuffers.Builder, tx app.Transaction) flatbuffers.UOffsetT {
	custID := b.CreateString(tx.Customer.ID)
	custNome := b.CreateString(tx.Customer.Nome)
	fb.CustomerStart(b)
	fb.CustomerAddId(b, custID)
	fb.CustomerAddNome(b, custNome)
	fb.CustomerAddCreateDate(b, tx.Customer.CreateDate.UnixMicro())
	cust := fb.CustomerEnd(b)

	cartID := b.CreateString(tx.CartSnapshot.ID)
	fb.CartSnapshotStart(b)
	fb.CartSnapshotAddId(b, cartID)
	fb.CartSnapshotAddCreateDate(b, tx.CartSnapshot.CreateDate.UnixMicro())
	cart := fb.CartSnapshotEnd(b)

	txID := b.CreateString(tx.ID)
	fb.TransactionStart(b)
	fb.TransactionAddId(b, txID)
	fb.TransactionAddValue(b, tx.Value)
	fb.TransactionAddCreateDate(b, tx.CreateDate.UnixMicro())
	fb.TransactionAddCustomer(b, cust)
	fb.TransactionAddCartSnapshot(b, cart)
	return fb.TransactionEnd(b)
}

// MarshalFlatTransaction returns a finished builder holding one Transaction.
func MarshalFlatTransaction(tx app.Transaction) *flatbuffers.Builder {
	b := flatbuffers.NewBuilder(256)
	b.Finish(buildFlatTransaction(b, tx))
	return b
}

// MarshalFlatList returns a finished builder holding a TransactionList.
func MarshalFlatList(txs []app.Transaction) *flatbuffers.Builder {
	b := flatbuffers.NewBuilder(1024)
	offs := make([]flatbuffers.UOffsetT, len(txs))
	for i, tx := range txs {
		offs[i] = buildFlatTransaction(b, tx)
	}
	fb.TransactionListStartTransactionsVector(b, len(txs))
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	vec := b.EndVector(len(txs))
	fb.TransactionListStart(b)
	fb.TransactionListAddTransactions(b, vec)
	b.Finish(fb.TransactionListEnd(b))
	return b
}

// MarshalFlatGetByID returns a finished builder holding a GetByIdRequest.
func MarshalFlatGetByID(id string) *flatbuffers.Builder {
	b := flatbuffers.NewBuilder(64)
	s := b.CreateString(id)
	fb.GetByIdRequestStart(b)
	fb.GetByIdRequestAddId(b, s)
	b.Finish(fb.GetByIdRequestEnd(b))
	return b
}

// MarshalFlatGetBatch returns a finished builder holding a GetBatchRequest.
func MarshalFlatGetBatch(limit int) *flatbuffers.Builder {
	b := flatbuffers.NewBuilder(32)
	fb.GetBatchRequestStart(b)
	fb.GetBatchRequestAddLimit(b, int32(limit))
	b.Finish(fb.GetBatchRequestEnd(b))
	return b
}

// ── readers (FlatBuffers → domain) ───────────────────────────────────────

// FlatToDomain reads a FlatBuffers Transaction table into the domain type.
func FlatToDomain(t *fb.Transaction) app.Transaction {
	var custTab fb.Customer
	var cartTab fb.CartSnapshot
	c := t.Customer(&custTab)
	cs := t.CartSnapshot(&cartTab)
	return app.Transaction{
		ID:         string(t.Id()),
		Value:      t.Value(),
		CreateDate: time.UnixMicro(t.CreateDate()).UTC(),
		Customer: app.Customer{
			ID:         string(c.Id()),
			Nome:       string(c.Nome()),
			CreateDate: time.UnixMicro(c.CreateDate()).UTC(),
		},
		CartSnapshot: app.CartSnapshot{
			ID:         string(cs.Id()),
			CreateDate: time.UnixMicro(cs.CreateDate()).UTC(),
		},
	}
}

// FlatListToDomain reads a FlatBuffers TransactionList into a domain slice.
func FlatListToDomain(l *fb.TransactionList) []app.Transaction {
	n := l.TransactionsLength()
	out := make([]app.Transaction, n)
	for i := 0; i < n; i++ {
		var t fb.Transaction
		l.Transactions(&t, i)
		out[i] = FlatToDomain(&t)
	}
	return out
}
