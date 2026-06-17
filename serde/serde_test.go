package serde

import (
	"reflect"
	"testing"
	"time"

	"example.com/httpdi/app"
)

// sampleTx uses UTC, micro-aligned timestamps so the Avro timestamp-micros
// logical type round-trips exactly (it truncates sub-microsecond precision).
func sampleTx() app.Transaction {
	return app.Transaction{
		ID:         "abc",
		Value:      100.50,
		CreateDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Customer: app.Customer{
			ID:         "cust-1",
			Nome:       "Customer #1",
			CreateDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		CartSnapshot: app.CartSnapshot{
			ID:         "cart-1",
			CreateDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	}
}

func TestCodecRoundTrip(t *testing.T) {
	codecs := map[string]Codec{
		"json":     JSON,
		"protobuf": Protobuf,
		"avro":     Avro,
	}
	want := sampleTx()

	for name, codec := range codecs {
		t.Run(name, func(t *testing.T) {
			data, err := codec.Marshal(want)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got, err := codec.Unmarshal(data)
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestForAccept(t *testing.T) {
	tests := []struct {
		accept string
		want   string
	}{
		{ContentJSON, ContentJSON},
		{ContentProtobuf, ContentProtobuf},
		{ContentAvro, ContentAvro},
		{"", ContentJSON},
		{"text/html", ContentJSON},
	}
	for _, tt := range tests {
		if got := ForAccept(tt.accept).ContentType(); got != tt.want {
			t.Errorf("ForAccept(%q).ContentType() = %q, want %q", tt.accept, got, tt.want)
		}
	}
}
