package minecraft

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestNetherNetDefaultHTTPPingAndClose(t *testing.T) {
	network, ok := networkByID("nethernet", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !ok {
		t.Fatal("NetherNet network is not registered")
	}
	listener, err := network.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	address := listener.Addr().String()
	want := endpoint.Status{
		ServerName: "transport test", Protocol: protocol.CurrentProtocol, Version: protocol.CurrentVersion,
		LevelName: "test", PlayerCount: 2, MaxPlayerCount: 32, GameType: endpoint.GameTypeAdventure,
	}
	listener.PongData(want.RakNet())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pong, err := network.PingContext(ctx, "http://"+address)
	if err != nil {
		t.Fatal(err)
	}
	got, err := endpoint.RakNetPongData(pong)
	if err != nil || got != want {
		t.Fatalf("HTTP pong: got %+v (%v), want %+v", got, err, want)
	}
	accepted := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		accepted <- err
	}()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-accepted:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("accept after close: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("close did not unblock accept")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err == nil {
		_ = conn.Close()
		t.Fatal("owned HTTP listener remained open after NetherNet close")
	}
}

func TestNetherNetExplicitSignalingRemainsCallerOwned(t *testing.T) {
	server, err := endpoint.Serve("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	network := NetherNet{Signaling: server, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	listener, err := network.Listen("ignored")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	listener.PongData((endpoint.Status{ServerName: "caller owned", Version: protocol.CurrentVersion}).RakNet())
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := (NetherNet{}).PingContext(ctx, "http://"+server.Addr().String()); err != nil {
		t.Fatalf("closing NetherNet also closed caller-owned signaling: %v", err)
	}
}

func TestNetherNetDefaultHTTPBindFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	listener, err := (NetherNet{}).Listen(occupied.Addr().String())
	if err == nil {
		_ = listener.Close()
		t.Fatal("NetherNet succeeded on an occupied HTTP port")
	}
	if listener != nil {
		t.Fatal("failed bind returned a listener")
	}
}

func TestNetherNetDefaultPingCancellation(t *testing.T) {
	server := endpoint.NewHandler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A cancelled request must terminate before making any network connection.
	if _, err := (NetherNet{}).PingContext(ctx, "http://127.0.0.1:1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled HTTP ping: %v", err)
	}
	// A caller-provided signaling handler does not implement outbound HTTP pings.
	if _, err := (NetherNet{Signaling: server}).PingContext(ctx, "http://127.0.0.1:1"); err == nil {
		t.Fatal("handler unexpectedly supports outbound pings")
	}
}
