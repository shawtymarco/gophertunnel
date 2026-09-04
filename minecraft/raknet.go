package minecraft

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/sandertv/go-raknet"
)

const (
	rakNetProtocolV10          byte = 10
	rakNetProtocolCurrent      byte = 11
	openConnectionRequest1          = 0x05
	openConnectionProtocolByte      = 17
)

// RakNet is the native RakNet network. Its listener also accepts the wire-
// compatible v10 handshake while keeping the current upstream implementation.
type RakNet struct {
	l          *slog.Logger
	maximumMTU uint16
}

// RakNetV10 is a development dialer network that emits a v10 handshake.
type RakNetV10 struct {
	l          *slog.Logger
	maximumMTU uint16
}

// WithMaximumMTU returns a copy of r that caps the largest RakNet MTU negotiated. A zero maximum keeps
// go-raknet's default.
func (r RakNet) WithMaximumMTU(maximumMTU uint16) Network {
	r.maximumMTU = maximumMTU
	return r
}

// WithMaximumMTU returns a copy of r that caps the largest RakNet MTU negotiated. A zero maximum keeps
// go-raknet's default.
func (r RakNetV10) WithMaximumMTU(maximumMTU uint16) Network {
	r.maximumMTU = maximumMTU
	return r
}

func (r RakNet) DialContext(ctx context.Context, address string) (net.Conn, error) {
	return raknet.Dialer{ErrorLog: r.l.With("net origin", "raknet"), MaxMTU: r.maximumMTU}.DialContext(ctx, address)
}

func (r RakNet) PingContext(ctx context.Context, address string) ([]byte, error) {
	return raknet.Dialer{ErrorLog: r.l.With("net origin", "raknet")}.PingContext(ctx, address)
}

func (r RakNet) Listen(address string) (NetworkListener, error) {
	return listenMultiVersionRakNet(address, r.l.With("net origin", "raknet"), r.maximumMTU)
}

func (r RakNetV10) DialContext(ctx context.Context, address string) (net.Conn, error) {
	return raknet.Dialer{
		ErrorLog:       r.l.With("net origin", "raknet-v10"),
		UpstreamDialer: rakNetV10UpstreamDialer{logger: r.l.With("net origin", "raknet-v10")},
		MaxMTU:         r.maximumMTU,
	}.DialContext(ctx, address)
}

func (r RakNetV10) PingContext(ctx context.Context, address string) ([]byte, error) {
	return raknet.Dialer{ErrorLog: r.l.With("net origin", "raknet-v10")}.PingContext(ctx, address)
}

func (r RakNetV10) Listen(address string) (NetworkListener, error) {
	return listenMultiVersionRakNet(address, r.l.With("net origin", "raknet-v10"), r.maximumMTU)
}

func listenMultiVersionRakNet(address string, logger *slog.Logger, maximumMTU uint16) (NetworkListener, error) {
	tracker := newRakNetVersionTracker()
	listener, err := (raknet.ListenConfig{
		ErrorLog:               logger,
		UpstreamPacketListener: rakNetPacketListener{tracker: tracker, logger: logger},
		MaxMTU:                 maximumMTU,
	}).Listen(address)
	if err != nil {
		return nil, err
	}
	return &versionedRakNetListener{Listener: listener, tracker: tracker, logger: logger}, nil
}

type versionedRakNetListener struct {
	*raknet.Listener
	tracker *rakNetVersionTracker
	logger  *slog.Logger
}

func (listener *versionedRakNetListener) Accept() (net.Conn, error) {
	conn, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	rakNetConn, ok := conn.(*raknet.Conn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("accept RakNet connection: unexpected type %T", conn)
	}
	version := listener.tracker.take(conn.RemoteAddr())
	listener.logger.Debug("accepted RakNet connection", "protocol", version, "raddr", conn.RemoteAddr())
	return &versionedRakNetConn{Conn: rakNetConn, version: version}, nil
}

type versionedRakNetConn struct {
	*raknet.Conn
	version byte
}

func (conn *versionedRakNetConn) ProtocolVersion() byte { return conn.version }

type rakNetPacketListener struct {
	tracker *rakNetVersionTracker
	logger  *slog.Logger
}

func (listener rakNetPacketListener) ListenPacket(network, address string) (net.PacketConn, error) {
	conn, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	return &rakNetPacketConn{PacketConn: conn, tracker: listener.tracker, logger: listener.logger}, nil
}

type rakNetPacketConn struct {
	net.PacketConn
	tracker *rakNetVersionTracker
	logger  *slog.Logger
}

func (conn *rakNetPacketConn) ReadFrom(buffer []byte) (int, net.Addr, error) {
	count, address, err := conn.PacketConn.ReadFrom(buffer)
	if err != nil || count <= openConnectionProtocolByte || buffer[0] != openConnectionRequest1 {
		return count, address, err
	}
	if buffer[openConnectionProtocolByte] == rakNetProtocolV10 {
		conn.logger.Debug("received RakNet v10 handshake", "raddr", address)
		conn.tracker.remember(address, rakNetProtocolV10)
		buffer[openConnectionProtocolByte] = rakNetProtocolCurrent
	}
	return count, address, nil
}

type rakNetV10UpstreamDialer struct{ logger *slog.Logger }

func (dialer rakNetV10UpstreamDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &rakNetV10DialConn{Conn: conn, logger: dialer.logger}, nil
}

type rakNetV10DialConn struct {
	net.Conn
	logger *slog.Logger
}

func (conn *rakNetV10DialConn) Write(buffer []byte) (int, error) {
	if len(buffer) <= openConnectionProtocolByte || buffer[0] != openConnectionRequest1 {
		return conn.Conn.Write(buffer)
	}
	cloned := append([]byte(nil), buffer...)
	cloned[openConnectionProtocolByte] = rakNetProtocolV10
	conn.logger.Debug("sending RakNet v10 handshake", "raddr", conn.RemoteAddr())
	return conn.Conn.Write(cloned)
}

type rakNetVersionRecord struct {
	version byte
	seen    time.Time
}

type rakNetVersionTracker struct {
	mu       sync.Mutex
	versions map[string]rakNetVersionRecord
}

func newRakNetVersionTracker() *rakNetVersionTracker {
	return &rakNetVersionTracker{versions: map[string]rakNetVersionRecord{}}
}

func (tracker *rakNetVersionTracker) remember(address net.Addr, version byte) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.versions) >= 1024 {
		cutoff := time.Now().Add(-30 * time.Second)
		for key, record := range tracker.versions {
			if record.seen.Before(cutoff) {
				delete(tracker.versions, key)
			}
		}
	}
	tracker.versions[address.String()] = rakNetVersionRecord{version: version, seen: time.Now()}
}

func (tracker *rakNetVersionTracker) take(address net.Addr) byte {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	key := address.String()
	record, ok := tracker.versions[key]
	delete(tracker.versions, key)
	if ok {
		return record.version
	}
	return rakNetProtocolCurrent
}

func init() {
	RegisterNetwork("raknet", func(l *slog.Logger) Network { return RakNet{l: l} })
	RegisterNetwork("raknet-v10", func(l *slog.Logger) Network { return RakNetV10{l: l} })
}
