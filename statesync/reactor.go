package statesync

import (
	// nolint: gosec
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"

	amino "github.com/tendermint/go-amino"
	"github.com/tendermint/tendermint/abci/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/proxy"
)

const (
	MetadataChannel = byte(0x60) // Transfers metadata about snapshots and channels
	ChunkChannel    = byte(0x61) // Transfers snapshot chunks

	maxMsgSize      = int(65e6)
	metadataMsgSize = 16e6
	//snapshotMetadataLimit = 16e3
	chunkMsgSize = 64e6
)

// Reactor handles state sync, both restoring snapshots for the local node and also
// serving snapshots for peers doing state sync.
type Reactor struct {
	p2p.BaseReactor
	config *cfg.StateSyncConfig
	conn   proxy.AppConnSnapshot

	mtxRestore sync.Mutex
	restoring  *Snapshot
	nextChunk  uint64
	source     *p2p.Peer
}

// NewReactor returns a new state sync reactor.
func NewReactor(config *cfg.StateSyncConfig, conn proxy.AppConnSnapshot) *Reactor {
	ssR := &Reactor{
		config: config,
		conn:   conn,
	}
	ssR.BaseReactor = *p2p.NewBaseReactor("StateSyncReactor", ssR)
	return ssR
}

// OnStart implements p2p.BaseReactor.
func (ssR *Reactor) OnStart() error {
	ssR.Logger.Info("Starting state sync reactor")
	if !ssR.config.Enabled {
		ssR.Logger.Info("State sync disabled")
		return nil
	}
	return nil
}

// GetChannels implements Reactor
func (ssR *Reactor) GetChannels() []*p2p.ChannelDescriptor {
	return []*p2p.ChannelDescriptor{
		{
			ID:                  MetadataChannel,
			Priority:            3,
			SendQueueCapacity:   100,
			RecvMessageCapacity: metadataMsgSize,
		},
		{
			ID:                  ChunkChannel,
			Priority:            1,
			SendQueueCapacity:   4,
			RecvMessageCapacity: chunkMsgSize,
		},
	}
}

// Receive implements Reactor
func (ssR *Reactor) Receive(chID byte, src p2p.Peer, msgBytes []byte) {
	if !ssR.IsRunning() {
		return
	}

	msg, err := decodeMsg(msgBytes)
	if err != nil {
		ssR.Logger.Error("Error decoding message", "src", src, "chId", chID, "msg", msg, "err", err, "bytes", msgBytes)
		ssR.Switch.StopPeerForError(src, err)
		return
	}

	if err = msg.ValidateBasic(); err != nil {
		ssR.Logger.Error("Peer sent us invalid msg", "peer", src, "msg", msg, "err", err)
		ssR.Switch.StopPeerForError(src, err)
		return
	}

	ssR.Logger.Debug("Receive", "src", src, "chId", chID, "msg", msg)
	switch chID {
	case MetadataChannel:
		switch msg := msg.(type) {
		case *ListSnapshotsRequestMessage:
			resp, err := ssR.conn.ListSnapshotsSync(types.RequestListSnapshots{})
			if err != nil {
				ssR.Logger.Error("Failed to list snapshots", "err", err)
				return
			}
			snapshots := make([]Snapshot, 0, len(resp.Snapshots))
			for _, snapshot := range resp.Snapshots {
				// FIXME Should have conversion function
				snapshots = append(snapshots, Snapshot{
					Height:   snapshot.Height,
					Format:   snapshot.Format,
					Chunks:   snapshot.Chunks,
					Metadata: snapshot.Metadata,
				})
			}
			src.Send(MetadataChannel, cdc.MustMarshalBinaryBare(&ListSnapshotsResponseMessage{
				Snapshots: snapshots,
			}))
		case *ListSnapshotsResponseMessage:
			ssR.mtxRestore.Lock()
			defer ssR.mtxRestore.Unlock()
			if ssR.restoring != nil {
				return
			}
			ssR.Logger.Info(fmt.Sprintf("Received %v snapshots", len(msg.Snapshots)), "peer", src.ID())
			if len(msg.Snapshots) == 0 {
				return
			}
			snapshots := msg.Snapshots
			sort.Slice(snapshots, func(i, j int) bool {
				a, b := snapshots[i], snapshots[j]
				switch {
				case a.Height < b.Height:
					return false
				case a.Height == b.Height && a.Format < b.Format:
					return false
				default:
					return true
				}
			})
			for _, snapshot := range snapshots {
				ssR.Logger.Info("Offering snapshot", "height", snapshot.Height, "format", snapshot.Format)
				resp, err := ssR.conn.OfferSnapshotSync(types.RequestOfferSnapshot{
					// FIXME Should have conversion function
					Snapshot: &types.Snapshot{
						Height:   snapshot.Height,
						Format:   snapshot.Format,
						Chunks:   snapshot.Chunks,
						Metadata: snapshot.Metadata,
					},
				})
				if err != nil {
					panic(err)
				}
				if resp.Accepted {
					ssR.Logger.Info("Accepted snapshot", "height", snapshot.Height, "format", snapshot.Format)
					s := snapshot
					ssR.restoring = &s
					ssR.source = &src
					ssR.nextChunk = 1
					ssR.Logger.Info("Fetching snapshot chunk", "peer", src.ID(), "chunk", ssR.nextChunk)
					(*ssR.source).Send(ChunkChannel, cdc.MustMarshalBinaryBare(&GetSnapshotChunkRequestMessage{
						Height: snapshot.Height,
						Format: snapshot.Format,
						Chunk:  1,
					}))
					break
				}
			}
		}
	case ChunkChannel:
		switch msg := msg.(type) {
		case *GetSnapshotChunkRequestMessage:
			ssR.Logger.Info("Providing snapshot chunk", "height", msg.Height, "format", msg.Format, "chunk", msg.Chunk)
			resp, err := ssR.conn.GetSnapshotChunkSync(types.RequestGetSnapshotChunk{
				Height: msg.Height,
				Format: msg.Format,
				Chunk:  msg.Chunk,
			})
			if err != nil {
				panic(err)
			}
			if resp.Chunk == nil {
				panic("No chunk")
			}
			// FIXME Verify checksum
			src.Send(ChunkChannel, cdc.MustMarshalBinaryBare(&GetSnapshotChunkResponseMessage{
				// FIXME Conversion
				Chunk: SnapshotChunk{
					Height:   resp.Chunk.Height,
					Format:   resp.Chunk.Format,
					Chunk:    resp.Chunk.Chunk,
					Data:     resp.Chunk.Data,
					Checksum: resp.Chunk.Checksum,
				},
			}))

		case *GetSnapshotChunkResponseMessage:
			ssR.mtxRestore.Lock()
			defer ssR.mtxRestore.Unlock()
			if ssR.restoring == nil {
				ssR.Logger.Error("Received chunk with no restore in progress")
				return
			}
			if msg.Chunk.Height != ssR.restoring.Height {
				ssR.Logger.Error("Received chunk for other height")
				return
			}
			if msg.Chunk.Format != ssR.restoring.Format {
				ssR.Logger.Error("Received chunk for other format")
				return
			}
			if msg.Chunk.Chunk != ssR.nextChunk {
				ssR.Logger.Error(fmt.Sprintf("Received chunk %v, expected %v", msg.Chunk.Chunk, ssR.nextChunk))
				return
			}
			// FIXME Verify checksum
			ssR.Logger.Info(fmt.Sprintf("Applying chunk %v", msg.Chunk.Chunk))
			resp, err := ssR.conn.ApplySnapshotChunkSync(types.RequestApplySnapshotChunk{
				// FIXME Conversion
				Chunk: &types.SnapshotChunk{
					Height:   msg.Chunk.Height,
					Format:   msg.Chunk.Format,
					Chunk:    msg.Chunk.Chunk,
					Data:     msg.Chunk.Data,
					Checksum: msg.Chunk.Checksum,
				},
			})
			if err != nil {
				panic(err)
			}
			if !resp.Applied {
				// FIXME Retry from different peer or something
				ssR.Logger.Error(fmt.Sprintf("Failed to apply chunk %v", msg.Chunk.Chunk))
				return
			}
			ssR.nextChunk++
			if ssR.nextChunk >= ssR.restoring.Chunks {
				ssR.Logger.Info("Restore complete")
				return
			}
			ssR.Logger.Info("Fetching snapshot chunk", "peer", src.ID(), "chunk", ssR.nextChunk)
			(*ssR.source).Send(ChunkChannel, cdc.MustMarshalBinaryBare(&GetSnapshotChunkRequestMessage{
				Height: ssR.restoring.Height,
				Format: ssR.restoring.Format,
				Chunk:  ssR.nextChunk,
			}))
		}
	}
}

// AddPeer implements Reactor
func (ssR *Reactor) AddPeer(peer p2p.Peer) {
	ssR.Logger.Info(fmt.Sprintf("Found peer %q", peer.NodeInfo().ID()))
	go func() {
		for peer.IsRunning() && ssR.restoring == nil {
			ssR.Logger.Info(fmt.Sprintf("Requesting snapshots from %q", peer.ID()))
			res := peer.Send(MetadataChannel, cdc.MustMarshalBinaryBare(&ListSnapshotsRequestMessage{}))
			if !res {
				ssR.Logger.Error("Failed to send message", "peer", peer.ID())
			}
			time.Sleep(10 * time.Second)
		}
		ssR.Logger.Info(fmt.Sprintf("No longer soliciting snapshots from %q", peer.ID()))
	}()
}

// RemovePeer implements Reactor
func (ssR *Reactor) RemovePeer(peer p2p.Peer, reason interface{}) {
	ssR.Logger.Info(fmt.Sprintf("Removing peer %q", peer.NodeInfo().ID()))
}

//-----------------------------------------------------------------------------
// Messages

// Message is a message that can be sent and received on the Reactor
type Message interface {
	ValidateBasic() error
}

func decodeMsg(bz []byte) (msg Message, err error) {
	if len(bz) > maxMsgSize {
		return msg, fmt.Errorf("msg exceeds max size (%d > %d)", len(bz), maxMsgSize)
	}
	err = cdc.UnmarshalBinaryBare(bz, &msg)
	return
}

func RegisterMessages(cdc *amino.Codec) {
	cdc.RegisterInterface((*Message)(nil), nil)
	cdc.RegisterConcrete(&ListSnapshotsRequestMessage{}, "tendermint/ListSnapshotsRequestMessage", nil)
	cdc.RegisterConcrete(&ListSnapshotsResponseMessage{}, "tendermint/ListSnapshotsResponseMessage", nil)
	cdc.RegisterConcrete(&GetSnapshotChunkRequestMessage{}, "tendermint/GetSnapshotChunkRequestMessage", nil)
	cdc.RegisterConcrete(&GetSnapshotChunkResponseMessage{}, "tendermint/GetSnapshotChunkResponseMessage", nil)
}

// FIXME This should possibly be in /types/
type Snapshot struct {
	Height   uint64
	Format   uint32
	Chunks   uint64
	Metadata []byte
}

func (s *Snapshot) ValidateBasic() error {
	if s == nil {
		return errors.New("snapshot cannot be nil")
	}
	if s.Height == 0 {
		return errors.New("snapshot height cannot be 0")
	}
	return nil
}

type SnapshotChunk struct {
	Height   uint64
	Format   uint32
	Chunk    uint64
	Data     []byte
	Checksum []byte
}

func (c *SnapshotChunk) ValidateBasic() error {
	if c == nil {
		return errors.New("chunk cannot be nil")
	}
	if c.Height == 0 {
		return errors.New("chunk height cannot be 0")
	}
	if c.Chunk == 0 {
		return errors.New("chunk index cannot be 0")
	}
	return nil
}

type ListSnapshotsRequestMessage struct{}

func (m *ListSnapshotsRequestMessage) ValidateBasic() error {
	return nil
}

type ListSnapshotsResponseMessage struct {
	Snapshots []Snapshot
}

func (m *ListSnapshotsResponseMessage) ValidateBasic() error {
	if m == nil {
		return errors.New("nil message")
	}
	for _, snapshot := range m.Snapshots {
		err := snapshot.ValidateBasic()
		if err != nil {
			return err
		}
	}
	return nil
}

type GetSnapshotChunkRequestMessage struct {
	Height uint64
	Format uint32
	Chunk  uint64
}

func (m *GetSnapshotChunkRequestMessage) ValidateBasic() error {
	if m == nil {
		return errors.New("nil message")
	}
	if m.Height == 0 {
		return errors.New("height 0")
	}
	if m.Chunk == 0 {
		return errors.New("chunk 0")
	}
	return nil
}

type GetSnapshotChunkResponseMessage struct {
	Chunk SnapshotChunk
}

func (m *GetSnapshotChunkResponseMessage) ValidateBasic() error {
	if m == nil {
		return errors.New("chunk cannot be nil")
	}
	return m.Chunk.ValidateBasic()
}
