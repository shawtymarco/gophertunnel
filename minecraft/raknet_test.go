package minecraft

import (
	"context"
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
