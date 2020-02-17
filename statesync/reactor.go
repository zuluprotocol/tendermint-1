package statesync

import (
	"crypto/sha1" // nolint: gosec
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/pkg/errors"

	amino "github.com/tendermint/go-amino"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	bcRv0 "github.com/tendermint/tendermint/blockchain/v0"
	cfg "github.com/tendermint/tendermint/config"
	lite "github.com/tendermint/tendermint/lite2"
	"github.com/tendermint/tendermint/lite2/provider"
	httpp "github.com/tendermint/tendermint/lite2/provider/http"
	litedb "github.com/tendermint/tendermint/lite2/store/db"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/proxy"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/store"
	"github.com/tendermint/tendermint/types"
	db "github.com/tendermint/tm-db"
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
	config       *cfg.StateSyncConfig
	enabled      bool
	conn         proxy.AppConnSnapshot
	sync         Sync
	initialState sm.State
	lightClient  *lite.Client
	header       *types.SignedHeader
	stateDB      db.DB
	blockStore   *store.BlockStore
}

// NewReactor returns a new state sync reactor.
func NewReactor(
	config *cfg.StateSyncConfig,
	conn proxy.AppConnSnapshot,
	initialState sm.State,
	stateDB db.DB,
	blockStore *store.BlockStore) *Reactor {
	ssR := &Reactor{
		config:       config,
		enabled:      config.Enabled,
		conn:         conn,
		sync:         NewSync(conn),
		initialState: initialState,
		stateDB:      stateDB,
		blockStore:   blockStore,
	}
	ssR.BaseReactor = *p2p.NewBaseReactor("StateSyncReactor", ssR)
	return ssR
}

// OnStart implements p2p.BaseReactor.
func (ssR *Reactor) OnStart() error {
	ssR.Logger.Info("Starting state sync reactor")
	if !ssR.enabled {
		ssR.Logger.Info("State sync disabled")
		return nil
	}

	// Start looking for a verification source
	err := ssR.StartLightClient()
	if err != nil {
		ssR.Logger.Error(fmt.Sprintf("Failed to start light client: %v", err.Error()))
	}

	// Start a timeout to move to fast sync if no sync starts within 5 seconds
	/*go func() {
		time.Sleep(7 * time.Second)
		if !ssR.sync.IsActive() && !ssR.sync.IsDone() {
			// FIXME Only switch to fast sync if it is enabled, otherwise go straight to consensus
			ssR.Logger.Info("Timed out looking for snapshots, starting fast sync")
			ssR.SwitchToFastSync(nil, nil, nil)
		}
	}()*/
	return nil
}

// StartLightClient starts a light client
func (ssR *Reactor) StartLightClient() error {
	hash, err := hex.DecodeString(ssR.config.VerifyHash)
	if err != nil {
		return err
	}
	// FIXME Don't hardcode
	primary, err := httpp.New(ssR.initialState.ChainID, "http://192.168.10.2:26657")
	if err != nil {
		return err
	}
	w1, err := httpp.New(ssR.initialState.ChainID, "http://192.168.10.3:26657")
	if err != nil {
		return err
	}
	w2, err := httpp.New(ssR.initialState.ChainID, "http://192.168.10.4:26657")
	if err != nil {
		return err
	}
	ssR.Logger.Info("Light client create")
	lc, err := lite.NewClient(
		ssR.initialState.ChainID,
		lite.TrustOptions{
			Period: 21 * 24 * time.Hour,
			Height: ssR.config.VerifyHeight,
			Hash:   hash,
		},
		primary,
		[]provider.Provider{w1, w2},
		litedb.New(db.NewMemDB(), ""),
		lite.UpdatePeriod(0),
		lite.Logger(ssR.Logger),
	)
	if err != nil {
		return err
	}
	err = lc.Start()
	if err != nil {
		return err
	}
	ssR.lightClient = lc
	return nil
}

// SwitchToFastSync switches to fast sync
func (ssR *Reactor) SwitchToFastSync(state *sm.State) {
	ssR.enabled = false
	if ssR.lightClient != nil {
		ssR.Logger.Info("Stopping light client")
		ssR.lightClient.Stop()
		ssR.lightClient = nil
	}
	if bcR, ok := ssR.Switch.Reactor("BLOCKCHAIN").(*bcRv0.BlockchainReactor); ok {
		ssR.Logger.Info("Switching to fast sync")
		err := bcR.StartSync(state)
		if err != nil {
			ssR.Logger.Error("Failed to switch to fast sync", "err", err)
		}
	}
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
	err = msg.ValidateBasic()
	if err != nil {
		ssR.Logger.Error("Peer sent us invalid msg", "peer", src, "msg", msg, "err", err)
		ssR.Switch.StopPeerForError(src, err)
		return
	}

	ssR.Logger.Debug("Receive", "src", src, "chId", chID, "msg", msg)
	switch chID {
	case MetadataChannel:
		switch msg := msg.(type) {
		case *ListSnapshotsRequestMessage:
			resp, err := ssR.conn.ListSnapshotsSync(abcitypes.RequestListSnapshots{})
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
			if !ssR.enabled || ssR.sync.IsActive() || ssR.sync.IsDone() {
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
			if ssR.lightClient == nil {
				ssR.Logger.Error("No active light client, ignoring snapshots")
				return
			}
			for _, snapshot := range snapshots {
				ssR.Logger.Info("Fetching verified app hash for snapshot", "height", snapshot.Height, "format", snapshot.Format)
				// FIXME We fetch height+1, since snapshot height is *after* height, but light client
				// app hash is before height was applied.
				header, err := ssR.lightClient.VerifyHeaderAtHeight(int64(snapshot.Height+1), time.Now().UTC())
				if err != nil {
					ssR.Logger.Error("Failed to fetch header", "err", err.Error())
					time.Sleep(time.Second)
					src.Send(MetadataChannel, cdc.MustMarshalBinaryBare(&ListSnapshotsRequestMessage{}))
					return
				}
				ssR.header = header
				ssR.Logger.Info("Found app hash", "app_hash", hex.EncodeToString(header.Header.AppHash))
				ssR.Logger.Info("Offering snapshot", "height", snapshot.Height, "format", snapshot.Format)
				err = ssR.sync.Start(&snapshot, header.Header.AppHash.Bytes())
				if err != nil {
					switch err {
					case ErrSnapshotRejected, ErrSnapshotRejectedFormat, ErrSnapshotRejectedHeight:
						ssR.Logger.Info("Rejected snapshot")
						continue
					default:
						panic(err)
					}
				}
				height, format, chunk := ssR.sync.NextChunk()
				ssR.Logger.Info("Accepted snapshot", "height", height, "format", format)
				ssR.Logger.Info("Fetching snapshot chunk", "peer", src.ID(), "chunk", chunk)
				src.Send(ChunkChannel, cdc.MustMarshalBinaryBare(&GetSnapshotChunkRequestMessage{
					Height: height,
					Format: format,
					Chunk:  chunk,
				}))
				break
			}
		}
	case ChunkChannel:
		switch msg := msg.(type) {
		case *GetSnapshotChunkRequestMessage:
			ssR.Logger.Info("Providing snapshot chunk", "height", msg.Height, "format", msg.Format, "chunk", msg.Chunk)
			resp, err := ssR.conn.GetSnapshotChunkSync(abcitypes.RequestGetSnapshotChunk{
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
			chunk := SnapshotChunk{
				Height: resp.Chunk.Height,
				Format: resp.Chunk.Format,
				Chunk:  resp.Chunk.Chunk,
				Data:   resp.Chunk.Data,
			}
			copy(chunk.Checksum[:], resp.Chunk.Checksum)
			src.Send(ChunkChannel, cdc.MustMarshalBinaryBare(&GetSnapshotChunkResponseMessage{Chunk: chunk}))

		case *GetSnapshotChunkResponseMessage:
			if !ssR.enabled {
				ssR.Logger.Error("Received chunk while disabled")
				return
			}
			if !ssR.sync.IsActive() {
				ssR.Logger.Error("Received chunk with no restore in progress")
				return
			}
			ssR.Logger.Info(fmt.Sprintf("Applying chunk %v", msg.Chunk.Chunk))
			err := ssR.sync.Apply(&msg.Chunk)
			if err != nil {
				panic(err)
			}
			if height, format, chunk := ssR.sync.NextChunk(); height > 0 {
				ssR.Logger.Info("Fetching snapshot chunk", "peer", src.ID(), "chunk", chunk)
				src.Send(ChunkChannel, cdc.MustMarshalBinaryBare(&GetSnapshotChunkRequestMessage{
					Height: height,
					Format: format,
					Chunk:  chunk,
				}))
			} else {
				ssR.Logger.Info("Snapshot restoration complete", "height", ssR.sync.snapshot.Height,
					"app_hash", hex.EncodeToString(ssR.sync.verifyAppHash))

				// The header we fetch is at Snapshot.Height + 1
				nextHeader := ssR.header

				curHeader, err := ssR.lightClient.TrustedHeader(ssR.header.Height-1, time.Now().UTC())
				if err != nil {
					ssR.Logger.Error("Failed to fetch cur commit header", "err", err.Error())
					return
				}

				prevHeader, err := ssR.lightClient.TrustedHeader(ssR.header.Height-2, time.Now().UTC())
				if err != nil {
					ssR.Logger.Error("Failed to fetch prev commit header", "err", err.Error())
					return
				}

				nextValidators, err := ssR.lightClient.TrustedValidatorSet(nextHeader.Height, time.Now().UTC())
				if err != nil {
					ssR.Logger.Error("Failed to fetch next validator set", "err", err.Error())
				}

				curValidators, err := ssR.lightClient.TrustedValidatorSet(curHeader.Height, time.Now().UTC())
				if err != nil {
					ssR.Logger.Error("Failed to fetch cur validator set", "err", err.Error())
				}

				prevValidators, err := ssR.lightClient.TrustedValidatorSet(prevHeader.Height, time.Now().UTC())
				if err != nil {
					ssR.Logger.Error("Failed to fetch prev validator set", "err", err.Error())
				}

				state := sm.State{
					Version: ssR.initialState.Version,
					ChainID: ssR.initialState.ChainID,

					LastBlockHeight: curHeader.Height,
					LastBlockID:     curHeader.Commit.BlockID,
					LastBlockTime:   curHeader.Time,

					NextValidators:              nextValidators,
					Validators:                  nextValidators,
					LastValidators:              curValidators,
					LastHeightValidatorsChanged: 1,

					ConsensusParams:                  ssR.initialState.ConsensusParams,
					LastHeightConsensusParamsChanged: 1,

					LastResultsHash: ssR.header.LastResultsHash,
					AppHash:         nextHeader.AppHash,
				}

				ssR.Logger.Info("Saving state", "height", state.LastBlockHeight)
				sm.SaveState(ssR.stateDB, state)
				ssR.blockStore.SaveSeenCommit(prevHeader.Height, prevHeader.Commit)
				ssR.blockStore.SaveSeenCommit(curHeader.Height, curHeader.Commit)
				ssR.blockStore.SaveSeenCommit(nextHeader.Height, nextHeader.Commit)
				sm.SaveValidatorsInfo(ssR.stateDB, 1, prevValidators)
				sm.SaveValidatorsInfo(ssR.stateDB, prevHeader.Height, prevValidators)
				sm.SaveValidatorsInfo(ssR.stateDB, curHeader.Height, curValidators)
				sm.SaveValidatorsInfo(ssR.stateDB, nextHeader.Height, nextValidators)
				ssR.SwitchToFastSync(&state)
			}
		}
	}
}

// AddPeer implements Reactor
func (ssR *Reactor) AddPeer(peer p2p.Peer) {
	if !ssR.enabled {
		return
	}

	ssR.Logger.Info(fmt.Sprintf("Found peer %q", peer.NodeInfo().ID()))
	ssR.Logger.Info(fmt.Sprintf("Requesting snapshots from %q", peer.ID()))
	res := peer.Send(MetadataChannel, cdc.MustMarshalBinaryBare(&ListSnapshotsRequestMessage{}))
	if !res {
		ssR.Logger.Error("Failed to send message", "peer", peer.ID())
	}
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

type SnapshotChunk struct { // nolint: go-lint
	Height   uint64
	Chunk    uint64
	Format   uint32
	Data     []byte
	Checksum [sha1.Size]byte
}

func (c *SnapshotChunk) ValidateBasic() error {
	if c == nil {
		return errors.New("chunk cannot be nil")
	}
	if c.Height == 0 {
		return errors.New("chunk height cannot be 0")
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
