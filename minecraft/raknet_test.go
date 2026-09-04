package minecraft

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestMultiVersionRakNetNativePing(t *testing.T) {
	network := RakNet{l: slog.Default()}
	listener, err := network.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.PongData([]byte("pong"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := network.PingContext(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "pong" {
		t.Fatalf("pong: got %q, want %q", got, "pong")
	}
}

func TestRakNetMaximumMTUNegotiation(t *testing.T) {
	listener, err := (ListenConfig{AuthenticationDisabled: true, MaximumMTU: 1200}).Listen("raknet", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	conn, err := net.Dial("udp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	request := make([]byte, 1492-28)
	request[0] = openConnectionRequest1
	copy(request[1:], []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78})
	request[openConnectionProtocolByte] = rakNetProtocolCurrent
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 64)
	n, err := conn.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	if n < 3 || response[0] != 0x06 {
		t.Fatalf("OpenConnectionReply1: got %x", response[:n])
	}
	if got := binary.BigEndian.Uint16(response[n-2 : n]); got != 1200 {
		t.Fatalf("negotiated maximum MTU: got %d, want 1200", got)
	}
}

func TestMultiVersionRakNetV10Dial(t *testing.T) {
	listenerNetwork := RakNet{l: slog.Default()}
	listener, err := listenerNetwork.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	errors := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errors <- err
			return
		}
		accepted <- conn
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := (RakNetV10{l: slog.Default()}).DialContext(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case err := <-errors:
		t.Fatal(err)
	case server := <-accepted:
		defer server.Close()
		versioned, ok := server.(interface{ ProtocolVersion() byte })
		if !ok || versioned.ProtocolVersion() != rakNetProtocolV10 {
			t.Fatalf("accepted RakNet protocol: got %T/%v, want v10", server, ok)
		}
	case <-ctx.Done():
		t.Fatal(context.Cause(ctx))
	}
}
