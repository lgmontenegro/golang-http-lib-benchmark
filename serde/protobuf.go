package serde

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"example.com/httpdi/app"
	pb "example.com/httpdi/proto/transactionspb"
)

// pbCodec is the protobuf implementation of Codec. It reuses the generated
// pb.Transaction message as the wire shape and converts to/from the domain
// aggregate, keeping protobuf coupling out of app.Transaction.
type pbCodec struct{}

func (pbCodec) ContentType() string { return ContentProtobuf }

func (pbCodec) Marshal(tx app.Transaction) ([]byte, error) {
	return proto.Marshal(TransactionToProto(tx))
}

func (pbCodec) Unmarshal(data []byte) (app.Transaction, error) {
	var msg pb.Transaction
	if err := proto.Unmarshal(data, &msg); err != nil {
		return app.Transaction{}, err
	}
	return TransactionFromProto(&msg), nil
}

func (pbCodec) MarshalList(txs []app.Transaction) ([]byte, error) {
	list := &pb.TransactionList{Transactions: make([]*pb.Transaction, len(txs))}
	for i, tx := range txs {
		list.Transactions[i] = TransactionToProto(tx)
	}
	return proto.Marshal(list)
}

func (pbCodec) UnmarshalList(data []byte) ([]app.Transaction, error) {
	var list pb.TransactionList
	if err := proto.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	txs := make([]app.Transaction, len(list.Transactions))
	for i, p := range list.Transactions {
		txs[i] = TransactionFromProto(p)
	}
	return txs, nil
}

// TransactionToProto maps the domain aggregate onto its protobuf message.
// Shared by pbCodec and the gRPC dataservice handler.
func TransactionToProto(tx app.Transaction) *pb.Transaction {
	return &pb.Transaction{
		Id:         tx.ID,
		Value:      tx.Value,
		CreateDate: timestamppb.New(tx.CreateDate),
		Customer: &pb.Customer{
			Id:         tx.Customer.ID,
			Nome:       tx.Customer.Nome,
			CreateDate: timestamppb.New(tx.Customer.CreateDate),
		},
		CartSnapshot: &pb.CartSnapshot{
			Id:         tx.CartSnapshot.ID,
			CreateDate: timestamppb.New(tx.CartSnapshot.CreateDate),
		},
	}
}

// TransactionFromProto maps a protobuf message back to the domain aggregate.
// Shared by pbCodec and the gRPC client adapter.
func TransactionFromProto(p *pb.Transaction) app.Transaction {
	return app.Transaction{
		ID:         p.GetId(),
		Value:      p.GetValue(),
		CreateDate: p.GetCreateDate().AsTime(),
		Customer: app.Customer{
			ID:         p.GetCustomer().GetId(),
			Nome:       p.GetCustomer().GetNome(),
			CreateDate: p.GetCustomer().GetCreateDate().AsTime(),
		},
		CartSnapshot: app.CartSnapshot{
			ID:         p.GetCartSnapshot().GetId(),
			CreateDate: p.GetCartSnapshot().GetCreateDate().AsTime(),
		},
	}
}
