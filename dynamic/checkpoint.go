package dynamic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// MaxCheckpointBytes bounds the complete desired-state checkpoint, including
// its magic, checksum and JSON payload. Checkpoint and Restore share this limit.
const MaxCheckpointBytes = 256 << 20

// MaxCheckpointIDBytes bounds the UTF-8 checkpoint identity independently of
// source geometry. Identities must be nonempty, valid UTF-8 strings.
const MaxCheckpointIDBytes = 4096

const checkpointEnvelopeBytes = 8 + sha256.Size

type checkpoint struct {
	Version                                uint32
	CheckpointID                           string
	Revision                               uint64
	Scene                                  geom.Scene
	Config                                 bake.Config
	Unloaded                               []artifact.TileKey
	SourceFingerprint, SettingsFingerprint string
}

// Checkpoint captures desired state for a host-coordinated checkpoint. The host
// writes entity and world records under the same checkpoint ID, then commits
// its manifest last. No runtime polygon references are persisted. Oversized
// identities or encoded checkpoints return an error, never an unrestorable blob.
func (w *World) Checkpoint(id string) ([]byte, error) {
	if err := validateCheckpointID(id); err != nil {
		return nil, err
	}
	w.mu.Lock()
	p := checkpoint{Version: 1, CheckpointID: id, Revision: w.revision, Scene: w.scene, Config: w.config, Unloaded: keySlice(w.unavailable)}
	w.mu.Unlock()
	sortKeys(p.Unloaded)
	p.Scene = p.Scene.Clone()
	p.Config.Revision = p.Revision
	var err error
	p.SourceFingerprint, err = p.Scene.Fingerprint()
	if err != nil {
		return nil, err
	}
	p.SettingsFingerprint, err = p.Config.Fingerprint()
	if err != nil {
		return nil, err
	}
	return encodeCheckpoint(p, MaxCheckpointBytes)
}

func validateCheckpointID(id string) error {
	if id == "" || len(id) > MaxCheckpointIDBytes || !utf8.ValidString(id) {
		return errors.New("dynamic: checkpoint ID must contain 1..4096 bytes of valid UTF-8")
	}
	return nil
}

// The internal limit makes exact envelope boundaries testable without allocating
// a production-sized scene. Both public entry points pass MaxCheckpointBytes.
func encodeCheckpoint(p checkpoint, limit int) ([]byte, error) {
	if err := validateCheckpointID(p.CheckpointID); err != nil {
		return nil, err
	}
	if limit < checkpointEnvelopeBytes {
		return nil, errors.New("dynamic: checkpoint size limit is too small")
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(body) > limit-checkpointEnvelopeBytes {
		return nil, fmt.Errorf("dynamic: checkpoint exceeds %d bytes encoded", limit)
	}
	sum := sha256.Sum256(body)
	out := make([]byte, 0, checkpointEnvelopeBytes+len(body))
	out = append(out, "NGSTATE1"...)
	out = append(out, sum[:]...)
	return append(out, body...), nil
}

// Restore validates a bounded desired-state checkpoint and synchronously
// rebuilds it through the pure-Go baker. Required source errors are never skipped.
func Restore(ctx context.Context, data []byte, expectedID string) (*World, error) {
	p, err := decodeCheckpoint(data, expectedID, MaxCheckpointBytes)
	if err != nil {
		return nil, err
	}
	p.Config.Revision = p.Revision
	// Streaming intent belongs to the saved desired state. Validate it before
	// baking, then restore it without counting a new user edit/revision.
	var unloaded map[artifact.TileKey]bool
	if len(p.Unloaded) > 0 {
		keys, err := bake.TileKeys(p.Config)
		if err != nil {
			return nil, err
		}
		valid := make(map[artifact.TileKey]bool, len(keys))
		for _, key := range keys {
			valid[key] = true
		}
		unloaded = make(map[artifact.TileKey]bool, len(p.Unloaded))
		for _, key := range p.Unloaded {
			if !valid[key] || unloaded[key] {
				return nil, fmt.Errorf("dynamic: invalid or duplicate unloaded checkpoint column %v", key)
			}
			unloaded[key] = true
		}
	}
	w, err := New(ctx, p.Scene, p.Config)
	if err != nil {
		return nil, err
	}
	if len(unloaded) > 0 {
		// w is still private: no lease/reader can observe the fully loaded
		// intermediate generation. Prepare/Commit publishes the complete saved
		// streaming view before Restore returns, even at MaxUint64 revision.
		w.unavailable = unloaded
		for key := range unloaded {
			w.dirty[key] = true
		}
		w.state = Pending
		c, err := w.Prepare(ctx)
		if err != nil {
			return nil, err
		}
		if err = w.Commit(c); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func decodeCheckpoint(data []byte, expectedID string, limit int) (checkpoint, error) {
	var p checkpoint
	if expectedID != "" {
		if err := validateCheckpointID(expectedID); err != nil {
			return p, err
		}
	}
	if len(data) < checkpointEnvelopeBytes || len(data) > limit || string(data[:8]) != "NGSTATE1" {
		return p, errors.New("dynamic: invalid checkpoint")
	}
	sum := sha256.Sum256(data[checkpointEnvelopeBytes:])
	if !bytes.Equal(sum[:], data[8:checkpointEnvelopeBytes]) {
		return p, errors.New("dynamic: checkpoint checksum mismatch")
	}
	dec := json.NewDecoder(bytes.NewReader(data[checkpointEnvelopeBytes:]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return p, errors.New("dynamic: trailing checkpoint data")
	}
	if p.Version != 1 || p.Revision == 0 || validateCheckpointID(p.CheckpointID) != nil || (expectedID != "" && p.CheckpointID != expectedID) {
		return p, errors.New("dynamic: checkpoint identity/version mismatch")
	}
	hash, err := p.Scene.Fingerprint()
	if err != nil || hash != p.SourceFingerprint {
		return p, errors.New("dynamic: source fingerprint mismatch")
	}
	hash, err = p.Config.Fingerprint()
	if err != nil || hash != p.SettingsFingerprint {
		return p, errors.New("dynamic: settings fingerprint mismatch")
	}
	return p, nil
}
