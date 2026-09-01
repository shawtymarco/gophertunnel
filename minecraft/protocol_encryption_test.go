package minecraft

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type encryptionTestProtocol struct {
	Protocol
	calls *atomic.Int32
}

func (protocol encryptionTestProtocol) Encryption(key [32]byte) packet.Encryption {
	protocol.calls.Add(1)
	return packet.NewCFB8Encryption(key)
}

func TestProtocolEncryptionFactoryUsedForBothDirections(t *testing.T) {
	var calls atomic.Int32
	conn := &Conn{
		proto: encryptionTestProtocol{Protocol: DefaultProtocol, calls: &calls},
		enc:   packet.NewEncoder(io.Discard),
		dec:   packet.NewDecoder(bytes.NewReader(nil)),
	}
	conn.enablePacketEncryption([32]byte{1, 2, 3})
	if got := calls.Load(); got != 2 {
		t.Fatalf("encryption factory calls: got %d, want 2", got)
	}
}
