package minecraft

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The transport discards encoded packets without starting a listener, dialer or
// background flush loop. The tests still exercise the actual packet writer.
type gameDataTestTransport struct{}

func (gameDataTestTransport) Read([]byte) (int, error)         { return 0, io.EOF }
func (gameDataTestTransport) Write(p []byte) (int, error)      { return len(p), nil }
func (gameDataTestTransport) Close() error                     { return nil }
func (gameDataTestTransport) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (gameDataTestTransport) RemoteAddr() net.Addr             { return &net.UDPAddr{} }
func (gameDataTestTransport) SetDeadline(time.Time) error      { return nil }
func (gameDataTestTransport) SetReadDeadline(time.Time) error  { return nil }
func (gameDataTestTransport) SetWriteDeadline(time.Time) error { return nil }

func gameDataTestConn(p Protocol) *Conn {
	return newConn(gameDataTestTransport{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), p, 0, false)
}

func gameDataFixture(id uint64) GameData {
	return GameData{
		WorldName: "fixture", EntityRuntimeID: id, EntityUniqueID: -int64(id), ChunkRadius: 8,
		GameRules: []protocol.GameRule{{Name: "doDaylightCycle", Value: false}},
		CustomBlocks: []protocol.BlockEntry{{Name: "test:block", Properties: map[string]any{
			"nested": map[string]any{"values": []int32{1, 2}},
		}}},
		Items:        []protocol.ItemEntry{{Name: "minecraft:shield", RuntimeID: 355, Data: map[string]any{"value": int32(1)}}},
		Experiments:  []protocol.ExperimentData{{Name: "test", Enabled: true}},
		PropertyData: map[string]any{"test": int32(2)},
		Dimensions:   []protocol.DimensionDefinition{{Name: "minecraft:overworld", Range: [2]int32{320, -64}}},
	}
}

func waitGameDataTasks(t *testing.T, group *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("game-data callback or concurrent access deadlocked")
	}
}

func checkGameDataSnapshot(t *testing.T, conn *Conn) {
	t.Helper()
	data := conn.GameData()
	if data.EntityUniqueID != -int64(data.EntityRuntimeID) {
		t.Errorf("torn game-data snapshot: unique=%d runtime=%d", data.EntityUniqueID, data.EntityRuntimeID)
	}
	if conn.ChunkRadius() < 0 {
		t.Error("negative chunk radius from a torn snapshot")
	}
	// Registry contents are shared immutable data. Read their backing arrays
	// as well as the copied headers while another handler replaces them.
	for _, item := range data.Items {
		if item.Name != "minecraft:shield" || item.Data["value"] != int32(1) {
			t.Error("item registry snapshot changed")
		}
	}
}

func TestConnGameDataConcurrentListenerInitialisation(t *testing.T) {
	conn := gameDataTestConn(DefaultProtocol)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var group sync.WaitGroup
	group.Add(3)
	go func() {
		defer group.Done()
		for i := range 200 {
			if err := conn.StartGameContext(ctx, gameDataFixture(uint64(i+1))); !errors.Is(err, context.Canceled) {
				t.Errorf("StartGameContext: %v", err)
				return
			}
		}
	}()
	go func() {
		defer group.Done()
		for i := range 200 {
			if err := conn.handleRequestChunkRadius(&packet.RequestChunkRadius{ChunkRadius: int32(i%32 + 1)}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer group.Done()
		for range 4000 {
			checkGameDataSnapshot(t, conn)
		}
	}()
	waitGameDataTasks(t, &group)
}

func TestConnGameDataConcurrentDialerPackets(t *testing.T) {
	conn := gameDataTestConn(DefaultProtocol)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		for i := range 500 {
			data := gameDataFixture(uint64(i + 1))
			if err := conn.handleDimensionData(&packet.DimensionData{Definitions: data.Dimensions}); err != nil {
				t.Error(err)
				return
			}
			if err := conn.handleStartGame(&packet.StartGame{EntityRuntimeID: data.EntityRuntimeID, EntityUniqueID: data.EntityUniqueID, PropertyData: data.PropertyData, GameRules: data.GameRules, Blocks: data.CustomBlocks}); err != nil {
				t.Error(err)
				return
			}
			if err := conn.handleItemRegistry(&packet.ItemRegistry{Items: data.Items}); err != nil {
				t.Error(err)
				return
			}
			if err := conn.handleChunkRadiusUpdated(&packet.ChunkRadiusUpdated{ChunkRadius: int32(i%32 + 1)}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer group.Done()
		for range 4000 {
			checkGameDataSnapshot(t, conn)
		}
	}()
	waitGameDataTasks(t, &group)
	if got := conn.GameData(); len(got.Dimensions) != 1 || got.Dimensions[0].Range != [2]int32{320, -64} || len(got.Items) != 1 {
		t.Fatalf("StartGame replaced pre-start dimensions or item registry: %+v", got)
	}
}

type gameDataCallbackProtocol struct {
	Protocol
	preSpawn func()
}

func (p gameDataCallbackProtocol) ConvertFromLatest(pk packet.Packet, conn *Conn) []packet.Packet {
	_ = conn.GameData()
	_ = conn.ChunkRadius()
	return p.Protocol.ConvertFromLatest(pk, conn)
}

func (p gameDataCallbackProtocol) PreSpawnPackets() []packet.Packet {
	p.preSpawn()
	return []packet.Packet{&packet.SetTime{Time: 123}}
}

func TestConnGameDataCallbacksPreserveSpawnPacketOrderAndInputs(t *testing.T) {
	var conn *Conn
	provider := gameDataCallbackProtocol{Protocol: DefaultProtocol, preSpawn: func() {
		if conn.GameData().ChunkRadius != 12 || conn.ChunkRadius() != 12 {
			t.Error("pre-spawn hook ran before publishing requested radius")
		}
	}}
	conn = gameDataTestConn(provider)
	var ids []uint32
	conn.packetFunc = func(header packet.Header, payload []byte, _, _ net.Addr) {
		_ = conn.GameData()
		_ = conn.ChunkRadius()
		ids = append(ids, header.PacketID)
		if header.PacketID == packet.IDChunkRadiusUpdated {
			var update packet.ChunkRadiusUpdated
			update.Marshal(protocol.NewReader(bytes.NewBuffer(payload), 0, false))
			if update.ChunkRadius != 8 || conn.ChunkRadius() != 8 {
				t.Error("configured radius or publication ordering changed")
			}
		}
	}
	data := gameDataFixture(99)
	request := &packet.RequestChunkRadius{ChunkRadius: 12, MaxChunkRadius: 16}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		if err := conn.StartGameContext(ctx, data); !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
		if err := conn.handleRequestChunkRadius(request); err != nil {
			t.Error(err)
		}
	}()
	waitGameDataTasks(t, &group)
	want := []uint32{packet.IDDimensionData, packet.IDJigsawStructureData, packet.IDVoxelShapes, packet.IDStartGame, packet.IDItemRegistry, packet.IDChunkRadiusUpdated, packet.IDSetTime, packet.IDPlayStatus, packet.IDCreativeContent}
	if !slices.Equal(ids, want) {
		t.Fatalf("spawn packet order changed: got %v want %v", ids, want)
	}
	if !reflect.DeepEqual(data, gameDataFixture(99)) || *request != (packet.RequestChunkRadius{ChunkRadius: 12, MaxChunkRadius: 16}) {
		t.Fatal("connection mutated supplied registry data or chunk-radius packet")
	}
	if err := conn.handleSetLocalPlayerAsInitialised(&packet.SetLocalPlayerAsInitialised{EntityRuntimeID: 98}); err == nil {
		t.Fatal("wrong initialised runtime ID was accepted")
	}
	if err := conn.handleSetLocalPlayerAsInitialised(&packet.SetLocalPlayerAsInitialised{EntityRuntimeID: 99}); err != nil {
		t.Fatal(err)
	}
}
