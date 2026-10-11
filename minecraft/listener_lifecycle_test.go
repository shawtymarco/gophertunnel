package minecraft

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestListenerCloseJoinsUnauthenticatedConnections(t *testing.T) {
	for _, network := range []Network{RakNet{}, RakNetV10{}} {
		t.Run(networkName(network), func(t *testing.T) {
			listener, err := (ListenConfig{AuthenticationDisabled: true}).ListenNetwork(network, "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			// go-raknet drains unacknowledged reliable frames for up to five
			// seconds on Close; the listener must join that transport cleanup.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			conn, err := network.DialContext(ctx, listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			deadline := time.Now().Add(2 * time.Second)
			for listener.PlayerCount() != 1 {
				if time.Now().After(deadline) {
					t.Fatal("raw connection was not counted")
				}
				time.Sleep(time.Millisecond)
			}
			accepted := make(chan error, 1)
			go func() { _, err := listener.Accept(); accepted <- err }()
			var wg sync.WaitGroup
			for range 4 {
				wg.Add(1)
				go func() { defer wg.Done(); _ = listener.Close() }()
			}
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("close did not join pending login")
			}
			if listener.PlayerCount() != 0 {
				t.Fatal("Close returned before player-count cleanup")
			}
			if err := <-accepted; !errors.Is(err, net.ErrClosed) {
				t.Fatalf("accept after close: %v", err)
			}
		})
	}
}

func networkName(n Network) string {
	if _, ok := n.(RakNetV10); ok {
		return "raknet-v10"
	}
	return "raknet"
}

func TestZeroValueRakNetPing(t *testing.T) {
	for _, network := range []Network{RakNet{}, RakNetV10{}} {
		listener, err := network.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listener.PongData([]byte("zero-value"))
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		got, err := network.PingContext(ctx, listener.Addr().String())
		cancel()
		_ = listener.Close()
		if err != nil || string(got) != "zero-value" {
			t.Fatalf("ping %q: %v", got, err)
		}
	}
}
