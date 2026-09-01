package packet

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestProtocolEncryptionRoundTrip(t *testing.T) {
	key := [32]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	plain := []byte{header, 1, 2, 3, 4, 5}
	for name, factory := range map[string]func([32]byte) Encryption{
		"ctr":  NewCTREncryption,
		"cfb8": NewCFB8Encryption,
	} {
		t.Run(name, func(t *testing.T) {
			encoded := factory(key).Encrypt(append([]byte(nil), plain...))
			if bytes.Equal(encoded, plain) {
				t.Fatal("encrypted batch equals plaintext")
			}
			decoded := append([]byte(nil), encoded[1:]...)
			decryptor := factory(key)
			decryptor.Decrypt(decoded)
			if err := decryptor.Verify(decoded); err != nil {
				t.Fatal(err)
			}
			if got := append([]byte{header}, decoded[:len(decoded)-8]...); !bytes.Equal(got, plain) {
				t.Fatalf("decoded batch differs: got %x, want %x", got, plain)
			}
		})
	}
}

func TestCFB8EncryptionMatchesProtocol419(t *testing.T) {
	key := [32]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	want, err := hex.DecodeString("fe5a919b47f931252dbceef9ed9c72cc3097cf934c64")
	if err != nil {
		t.Fatal(err)
	}
	var network bytes.Buffer
	encoder := NewEncoder(&network)
	encoder.EnableLegacyCompression(FlateCompression)
	encoder.EnableEncryptionWith(NewCFB8Encryption(key))
	if err := encoder.Encode([][]byte{{1, 2, 3, 4, 5}}); err != nil {
		t.Fatal(err)
	}
	if got := network.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("protocol 419 encryption bytes differ: got %x, want %x", got, want)
	}
}

func TestDefaultEncryptionRemainsCTR(t *testing.T) {
	key := [32]byte{1, 2, 3, 4}
	plain := []byte{header, 3, 9, 8, 7}
	var network bytes.Buffer
	encoder := NewEncoder(&network)
	encoder.EnableEncryption(key)
	if err := encoder.Encode([][]byte{plain[2:]}); err != nil {
		t.Fatal(err)
	}
	want := NewCTREncryption(key).Encrypt(append([]byte(nil), plain...))
	if got := network.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("default encryption changed: got %x, want %x", got, want)
	}
}
