package statesync

import (
	"crypto/sha1"
	"sync"

	"github.com/pkg/errors"

	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/proxy"
)

var (
	ErrSnapshotRejected       = errors.New("snapshot was rejected")
	ErrSnapshotRejectedFormat = errors.New("snapshot format was rejected")
	ErrSnapshotRejectedHeight = errors.New("snapshot height was rejected")
	ErrChunkChecksum          = errors.New("snapshot chunk checksum mismatch")
	ErrChunkVerify            = errors.New("snapshot chunk verification failed")
)

// Sync manages a state sync operation
type Sync struct {
	sync.Mutex
	conn          proxy.AppConnSnapshot
	snapshot      *Snapshot
	nextChunk     uint64
	verifyAppHash []byte
}

// NewSync creates a new Sync
func NewSync(conn proxy.AppConnSnapshot) Sync {
	return Sync{
		conn: conn,
	}
}

// IsActive checks whether the state sync is currently in progress.
func (s *Sync) IsActive() bool {
	s.Lock()
	defer s.Unlock()
	return s.snapshot != nil && s.nextChunk < s.snapshot.Chunks
}

// IsDone checks whether the state sync has completed.
func (s *Sync) IsDone() bool {
	s.Lock()
	defer s.Unlock()
	return s.snapshot != nil && s.nextChunk >= s.snapshot.Chunks
}

// NextChunk returns the height, format, and index for the next chunk, or
// all 0 if there is no active sync.
func (s *Sync) NextChunk() (uint64, uint32, uint64) {
	s.Lock()
	defer s.Unlock()
	if s.snapshot == nil || s.nextChunk >= s.snapshot.Chunks {
		return 0, 0, 0
	} else {
		return s.snapshot.Height, s.snapshot.Format, s.nextChunk
	}
}

// Start attempts to start a new sync operation by offering the snapshot
// to the state machine, returning an error if the snapshot is rejected.
func (s *Sync) Start(snapshot *Snapshot, verifyAppHash []byte) error {
	s.Lock()
	defer s.Unlock()

	if snapshot == nil {
		return errors.New("cannot sync nil snapshot")
	}
	if s.snapshot != nil {
		return errors.New("a state sync has already been started")
	}

	resp, err := s.conn.OfferSnapshotSync(types.RequestOfferSnapshot{
		// FIXME Should have conversion function
		Snapshot: &types.Snapshot{
			Height:   snapshot.Height,
			Format:   snapshot.Format,
			Chunks:   snapshot.Chunks,
			Metadata: snapshot.Metadata,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to offer snapshot to state machine")
	}
	if !resp.Accepted {
		switch resp.Reason {
		case types.ResponseOfferSnapshot_invalid_format:
			return ErrSnapshotRejectedFormat
		case types.ResponseOfferSnapshot_invalid_height:
			return ErrSnapshotRejectedHeight
		default:
			return ErrSnapshotRejected
		}
	}
	s.snapshot = snapshot
	s.nextChunk = 0
	s.verifyAppHash = verifyAppHash
	return nil
}

// Apply applies a chunk to the state machine.
func (s *Sync) Apply(chunk *SnapshotChunk) error {
	s.Lock()
	defer s.Unlock()

	if s.snapshot == nil {
		return errors.New("no state sync in progress")
	}
	if s.nextChunk >= s.snapshot.Chunks {
		return errors.New("state sync already completed")
	}
	if chunk == nil {
		return errors.New("received nil snapshot chunk")
	}
	if chunk.Height != s.snapshot.Height {
		return errors.Errorf("received snapshot chunk for height %v, expected %v",
			chunk.Height, s.snapshot.Height)
	}
	if chunk.Format != s.snapshot.Format {
		return errors.Errorf("received snapshot chunk with format %v, expected %v",
			chunk.Format, s.snapshot.Format)
	}
	if chunk.Chunk != s.nextChunk {
		return errors.Errorf("received snapshot chunk %v out of order, expected %v",
			chunk.Chunk, s.nextChunk)
	}
	if sha1.Sum(chunk.Data) != chunk.Checksum {
		return ErrChunkChecksum
	}

	resp, err := s.conn.ApplySnapshotChunkSync(types.RequestApplySnapshotChunk{
		// FIXME Should have conversion function or something
		Chunk: &types.SnapshotChunk{
			Height:   chunk.Height,
			Format:   chunk.Format,
			Chunk:    chunk.Chunk,
			Data:     chunk.Data,
			Checksum: chunk.Checksum[:],
		},
		// FIXME Rename to VerifyAppHash or something
		ChainHash: s.verifyAppHash,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to apply snapshot chunk %v", chunk.Chunk)
	}
	if !resp.Applied {
		switch resp.Reason {
		case types.ResponseApplySnapshotChunk_verify_failed:
			return ErrChunkVerify
		default:
			return errors.Errorf("failed to apply snapshot chunk %v", chunk.Chunk)
		}
	}
	s.nextChunk++
	return nil
}
