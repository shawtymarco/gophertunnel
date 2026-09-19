package minecraft

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type immediateSpawnTransport struct {
	gameDataTestTransport
	reply func()
	sent  bool
}

func (c *immediateSpawnTransport) Write(p []byte) (int, error) {
	if !c.sent {
		c.sent = true
		c.reply()
	}
	return len(p), nil
}

func TestStartGameAcceptsResponseBeforeFlushReturns(t *testing.T) {
	transport := &immediateSpawnTransport{}
	conn := newConn(transport, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultProtocol, 0, false)
	defer conn.Close()
	conn.pool = DefaultProtocol.Packets(true)
	conn.loggedIn = true
	radiusHandled, repliesDone := make(chan struct{}), make(chan struct{})
	var replyErr error
	conn.packetFunc = func(header packet.Header, _ []byte, _, _ net.Addr) {
		if header.PacketID == packet.IDChunkRadiusUpdated {
			close(radiusHandled)
		}
	}
	transport.reply = func() {
		// The peer may reply on the network reader while the first StartGame
		// batch is still being flushed. Receive must handle these internally;
		// deferring them lets an application reader discard the spawn handshake.
		go func() {
			defer close(repliesDone)
			for _, pk := range []packet.Packet{
				&packet.RequestChunkRadius{ChunkRadius: 8},
				&packet.SetLocalPlayerAsInitialised{EntityRuntimeID: 1},
			} {
				var buf bytes.Buffer
				if replyErr = (&packet.Header{PacketID: pk.ID()}).Write(&buf); replyErr != nil {
					return
				}
				pk.Marshal(protocol.NewWriter(&buf, -1))
				if replyErr = conn.receive(buf.Bytes()); replyErr != nil {
					return
				}
			}
		}()
		// The reader can queue its response while the original Flush is still
		// writing, but its own deferred Flush must wait for that write to end.
		select {
		case <-radiusHandled:
		case <-repliesDone:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := conn.StartGameContext(ctx, GameData{WorldName: "spawn-order", EntityRuntimeID: 1, EntityUniqueID: 1}); err != nil {
		t.Fatalf("immediate spawn replies were not handled: %v", err)
	}
	<-repliesDone
	if replyErr != nil {
		t.Fatal(replyErr)
	}
	if _, deferred := conn.takeDeferredPacket(); deferred {
		t.Fatal("spawn reply leaked into the application's packet queue")
	}
	if conn.waitingForSpawn.Load() || conn.ChunkRadius() != 8 {
		t.Fatal("spawn state did not complete with the requested chunk radius")
	}
}
