package minecraft

import (
	"fmt"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// BootstrapMode controls ownership of the connection sequence after authentication.
type BootstrapMode uint8

const (
	// BootstrapModeAutomatic negotiates packs and spawn using GameData, as usual.
	BootstrapModeAutomatic BootstrapMode = iota
	// BootstrapModeManual exposes the connection after authentication/encryption.
	// The caller owns packs, registries, cache negotiation and spawn. Use ReadPacket
	// and WritePacket: they maintain the item/shield registry without synthesising
	// acknowledgements, StartGame fields, or spawn packets. Authentication and
	// transport handshakes remain connection-owned.
	BootstrapModeManual
)

func (mode BootstrapMode) validate() error {
	if mode > BootstrapModeManual {
		return fmt.Errorf("invalid bootstrap mode %d", mode)
	}
	return nil
}

// observeManualPacket updates codec metadata without answering the peer. The
// caller retains the complete packet; GameData is only a convenience view.
func (conn *Conn) observeManualPacket(pk packet.Packet) {
	if conn.bootstrapMode != BootstrapModeManual {
		return
	}
	switch pk := pk.(type) {
	case *packet.StartGame:
		_ = conn.handleStartGame(pk)
	case *packet.ItemRegistry:
		conn.gameDataMu.Lock()
		conn.gameData.Items = pk.Items
		conn.gameDataMu.Unlock()
		for _, item := range pk.Items {
			if item.Name == "minecraft:shield" {
				conn.shieldID.Store(int32(item.RuntimeID))
			}
		}
	case *packet.DimensionData:
		_ = conn.handleDimensionData(pk)
	}
}
