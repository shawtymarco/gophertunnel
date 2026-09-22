package minecraft

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestConcurrentApplicationReceivesPreserveFIFO(t *testing.T) {
	const count = 10000
	encoded := make([][]byte, count)
	for i := range encoded {
		var buffer bytes.Buffer
		pk := &packet.Text{TextType: packet.TextTypeRaw, Message: fmt.Sprint(i)}
		if err := (&packet.Header{PacketID: pk.ID()}).Write(&buffer); err != nil {
			t.Fatal(err)
		}
		pk.Marshal(protocol.NewWriter(&buffer, 0))
		encoded[i] = buffer.Bytes()
	}
	for _, mode := range []string{"ReadPacket", "ReadBytes", "Read"} {
		t.Run(mode, func(t *testing.T) {
			conn := gameDataTestConn(DefaultProtocol)
			defer conn.Close()
			conn.pool = DefaultProtocol.Packets(false)
			conn.loggedIn = true
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			done := make(chan error, 1)
			go func() {
				for i, data := range encoded {
					if err := conn.receive(data); err != nil {
						done <- err
						return
					}
					if i%7 == 0 {
						runtime.Gosched()
					}
				}
				done <- nil
			}()
			buffer := make([]byte, 1024)
			for i, want := range encoded {
				var got []byte
				var err error
				switch mode {
				case "ReadPacket":
					var pk packet.Packet
					pk, err = conn.ReadPacket()
					if err == nil && pk.(*packet.Text).Message != fmt.Sprint(i) {
						t.Fatalf("packet %d overtaken by %s", i, pk.(*packet.Text).Message)
					}
				case "ReadBytes":
					got, err = conn.ReadBytes()
				case "Read":
					var n int
					n, err = conn.Read(buffer)
					got = buffer[:n]
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode != "ReadPacket" && !bytes.Equal(got, want) {
					t.Fatalf("packet %d was reordered", i)
				}
				if i%11 == 0 {
					runtime.Gosched()
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEmptyReceiveQueueHonoursDeadlineAndClose(t *testing.T) {
	conn := gameDataTestConn(DefaultProtocol)
	_ = conn.SetReadDeadline(time.Now().Add(time.Millisecond))
	if _, err := conn.ReadBytes(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.Close()
	if _, err := conn.ReadBytes(); err == nil {
		t.Fatal("closed read succeeded")
	}
}
