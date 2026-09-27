package minecraft

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestManualBootstrapEncryptedConnection(t *testing.T) {
	l, err := (ListenConfig{AuthenticationDisabled: true, BootstrapMode: BootstrapModeManual, FlushRate: time.Millisecond, AdvertisedProtocol: 486, AdvertisedVersion: "1.18.12"}).Listen("raknet", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan *Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		conn := c.(*Conn)
		accepted <- conn
		_ = conn.WritePacket(&packet.PlayStatus{Status: packet.PlayStatusLoginSuccess})
		_ = conn.Flush()
	}()
	c, err := (Dialer{BootstrapMode: BootstrapModeManual, FlushRate: time.Millisecond}).DialContext(ctx, "raknet", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := <-accepted
	defer s.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	first, err := c.ReadPacket()
	if err != nil || first.(*packet.PlayStatus).Status != packet.PlayStatusLoginSuccess {
		t.Fatalf("login: %T %v", first, err)
	}
	// No ResourcePacksInfo, cache status or spawn acknowledgement is automatic.
	_ = s.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err = s.ReadPacket(); err == nil {
		t.Fatal("unsolicited bootstrap acknowledgement")
	}
	_ = s.SetReadDeadline(time.Time{})
	packets := []packet.Packet{
		&packet.ResourcePacksInfo{TexturePackRequired: true},
		&packet.StartGame{WorldName: "original", CommandsEnabled: false, EducationFeaturesEnabled: false, EntityRuntimeID: 71, GameVersion: "1.26.50"},
		&packet.ItemRegistry{Items: []protocol.ItemEntry{{Name: "minecraft:shield", RuntimeID: 777}}},
		&packet.Text{TextType: packet.TextTypeRaw, Message: "after registry"},
		&packet.Disconnect{Message: "original reason", Reason: packet.DisconnectReasonKicked},
	}
	for _, pk := range packets {
		if err := s.WritePacket(pk); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	for i, want := range packets {
		got, err := c.ReadPacket()
		if err != nil || fmt.Sprintf("%T", got) != fmt.Sprintf("%T", want) {
			t.Fatalf("packet %d: %T %v", i, got, err)
		}
		if start, ok := got.(*packet.StartGame); ok && (start.CommandsEnabled || start.EducationFeaturesEnabled || start.EntityRuntimeID != 71) {
			t.Fatal("StartGame was reconstructed")
		}
		if disconnect, ok := got.(*packet.Disconnect); ok && disconnect.Message != "original reason" {
			t.Fatal("disconnect lost")
		}
	}
	if c.shieldID.Load() != 777 || c.GameData().WorldName != "original" {
		t.Fatal("codec metadata not updated")
	}
}

type splitProtocol struct{ Protocol }

func (splitProtocol) ConvertToLatest(packet.Packet, *Conn) []packet.Packet {
	out := make([]packet.Packet, 32)
	for i := range out {
		out[i] = &packet.Text{Message: fmt.Sprint(i)}
	}
	return out
}

func TestConversionCanYieldMoreThanSixteenPackets(t *testing.T) {
	c := gameDataTestConn(splitProtocol{DefaultProtocol})
	defer c.Close()
	c.pool = packet.NewServerPool()
	c.loggedIn = true
	var b bytes.Buffer
	pk := &packet.Text{TextType: packet.TextTypeRaw, Message: "split"}
	_ = (&packet.Header{PacketID: pk.ID()}).Write(&b)
	pk.Marshal(protocol.NewWriter(&b, 0))
	if err := c.receive(b.Bytes()); err != nil {
		t.Fatal(err)
	}
	for i := range 32 {
		p, err := c.ReadPacket()
		if err != nil || p.(*packet.Text).Message != fmt.Sprint(i) {
			t.Fatalf("split %d: %v", i, err)
		}
	}
}

func TestManualReceiveQueueBound(t *testing.T) {
	c := gameDataTestConn(DefaultProtocol)
	defer c.Close()
	c.bootstrapMode = BootstrapModeManual
	for range 4097 {
		c.deferPacket(&packetData{full: []byte{1}})
	}
	select {
	case <-c.Context().Done():
	default:
		t.Fatal("queue overflow did not close connection")
	}
}

func TestProtocolFactoryIsolation(t *testing.T) {
	factory := func(p Protocol) (Protocol, error) { return &splitProtocol{p}, nil }
	a, b := gameDataTestConn(DefaultProtocol), gameDataTestConn(DefaultProtocol)
	defer a.Close()
	defer b.Close()
	for _, c := range []*Conn{a, b} {
		c.acceptedProto = []Protocol{DefaultProtocol}
		c.protocolFactory = factory
		if !c.selectProtocol(protocol.CurrentProtocol) {
			t.Fatal("selection failed")
		}
	}
	if a.proto == b.proto {
		t.Fatal("factory shared instances")
	}
	if a.selectProtocol(999999) {
		t.Fatal("factory accepted unregistered protocol")
	}
}
