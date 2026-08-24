package packet

import (
	"bytes"
	"reflect"
	"testing"
)

func TestLegacyCompressionRoundTrip(t *testing.T) {
	want := [][]byte{{1, 2, 3, 4}, {5, 6}, {7}}
	var network bytes.Buffer
	encoder := NewEncoder(&network)
	encoder.EnableLegacyCompression(FlateCompression)
	if err := encoder.Encode(want); err != nil {
		t.Fatal(err)
	}
	framed := network.Bytes()
	if len(framed) < 2 || framed[0] != header {
		t.Fatalf("invalid legacy frame: %x", framed)
	}
	batch, err := FlateCompression.Decompress(framed[1:], 1<<20)
	if err != nil {
		t.Fatalf("decompress payload directly after the header: %v", err)
	}
	if wantBatch := []byte{4, 1, 2, 3, 4, 2, 5, 6, 1, 7}; !bytes.Equal(batch, wantBatch) {
		t.Fatalf("legacy frame contains an extra prefix: got %x, want %x", batch, wantBatch)
	}

	decoder := NewDecoder(bytes.NewReader(network.Bytes()))
	decoder.EnableLegacyCompression(FlateCompression, 1<<20)
	got, err := decoder.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded packets differ: got %v, want %v", got, want)
	}
}

func TestModernCompressionStillUsesAlgorithmPrefix(t *testing.T) {
	var network bytes.Buffer
	encoder := NewEncoder(&network)
	encoder.EnableCompression(FlateCompression, 0)
	if err := encoder.Encode([][]byte{{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	if got := network.Bytes(); len(got) < 2 || got[1] != byte(FlateCompression.EncodeCompression()) {
		t.Fatalf("modern frame has no algorithm prefix: %x", got)
	}
}
