package dynamic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func checkpointFixture(t testing.TB) checkpoint {
	t.Helper()
	p := checkpoint{Version: 1, CheckpointID: "size-boundary", Revision: 1,
		Config: bake.DefaultConfig("checkpoint-test", geom.Bounds{Min: geom.Vec3{Y: -1}, Max: geom.Vec3{X: 1, Y: 1, Z: 1}})}
	var err error
	p.SourceFingerprint, err = p.Scene.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	p.SettingsFingerprint, err = p.Config.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckpointAggregateEncodedSizeBoundary(t *testing.T) {
	p := checkpointFixture(t)
	encoded, err := encodeCheckpoint(p, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	// The reduced limit includes all 40 envelope bytes, not only the JSON body.
	limit := len(encoded)
	exact, err := encodeCheckpoint(p, limit)
	if err != nil || !bytes.Equal(exact, encoded) {
		t.Fatalf("exact-limit encode: %v", err)
	}
	if _, err = decodeCheckpoint(exact, p.CheckpointID, limit); err != nil {
		t.Fatalf("accepted checkpoint could not roundtrip: %v", err)
	}
	if blob, err := encodeCheckpoint(p, limit-1); err == nil || blob != nil {
		t.Fatal("writer returned a checkpoint that exceeds the shared envelope limit")
	}
	if _, err = decodeCheckpoint(encoded, p.CheckpointID, limit-1); err == nil {
		t.Fatal("reader accepted an oversized envelope")
	}
	for _, small := range []int{-1, 0, checkpointEnvelopeBytes - 1} {
		if _, err = encodeCheckpoint(p, small); err == nil {
			t.Fatalf("accepted impossible limit %d", small)
		}
	}
}

func TestCheckpointIdentityBoundedBeforeCaptureAndRestore(t *testing.T) {
	p := checkpointFixture(t)
	w := &World{scene: p.Scene, config: p.Config, revision: p.Revision}
	for _, id := range []string{"", strings.Repeat("x", MaxCheckpointIDBytes+1), string([]byte{0xff})} {
		if blob, err := w.Checkpoint(id); err == nil || blob != nil {
			t.Fatal("writer accepted invalid checkpoint identity")
		}
	}
	id := strings.Repeat("x", MaxCheckpointIDBytes)
	encoded, err := w.Checkpoint(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxCheckpointBytes {
		t.Fatal("public writer exceeded production limit")
	}
	if _, err = Restore(context.Background(), encoded, id); err != nil {
		t.Fatalf("maximum-length identity did not restore: %v", err)
	}
	if _, err = Restore(context.Background(), encoded, id+"x"); err == nil {
		t.Fatal("accepted oversized expected identity")
	}
	// Construct malformed wire data independently so decoder coverage does not
	// depend on the writer already rejecting an oversized embedded identity.
	p.CheckpointID = id + "x"
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	malformed := append([]byte("NGSTATE1"), sum[:]...)
	malformed = append(malformed, body...)
	if _, err = Restore(context.Background(), malformed, ""); err == nil {
		t.Fatal("reader accepted oversized embedded identity")
	}
}

func TestCheckpointRestorePreservesUnloadedRevision(t *testing.T) {
	for _, initialRevision := range []uint64{7, ^uint64(0) - 1} {
		t.Run(fmt.Sprint(initialRevision), func(t *testing.T) {
			p := checkpointFixture(t)
			p.Config.Revision = initialRevision
			world, err := New(context.Background(), p.Scene, p.Config)
			if err != nil {
				t.Fatal(err)
			}
			keys, err := bake.TileKeys(p.Config)
			if err != nil || len(keys) != 1 {
				t.Fatalf("fixture columns: %v, %v", keys, err)
			}
			if _, err = world.SetLoaded(keys, false); err != nil {
				t.Fatal(err)
			}
			// Capture pending intent as well as the maximum possible revision.
			before, err := world.Checkpoint("streaming")
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Restore(context.Background(), before, "streaming")
			if err != nil {
				t.Fatal(err)
			}
			if got := restored.Snapshot().Revision(); got != initialRevision+1 {
				t.Fatalf("restoration changed saved revision: got %d, want %d", got, initialRevision+1)
			}
			if restored.Pending() || restored.Status().State != Committed || len(restored.Status().Dirty) != 0 {
				t.Fatal("restoration returned before publishing saved streaming state")
			}
			bundle, err := restored.Snapshot().Bundle()
			if err != nil || len(bundle.Tiles) != 0 {
				t.Fatalf("unloaded columns were exposed: %d tiles, %v", len(bundle.Tiles), err)
			}
			after, err := restored.Checkpoint("streaming")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("restoration changed desired source/config/streaming state: %v", err)
			}
		})
	}
}

func TestCheckpointRestoreRejectsInvalidUnloadedColumns(t *testing.T) {
	for _, keys := range [][]artifact.TileKey{{{X: 100}}, {{}, {}}} {
		p := checkpointFixture(t)
		p.Unloaded = keys
		data, err := encodeCheckpoint(p, MaxCheckpointBytes)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Restore(context.Background(), data, p.CheckpointID); err == nil {
			t.Fatalf("accepted malformed streaming directory: %v", keys)
		}
	}
}

// FuzzCheckpointDecode keeps malformed-input work bounded and deliberately
// reseals fuzzed JSON so the checksum does not hide the deeper parsing paths.
// Baking is covered by restart regressions, not by this decoder-only target.
func FuzzCheckpointDecode(f *testing.F) {
	const limit = 8 << 10
	p := checkpointFixture(f)
	encoded, err := encodeCheckpoint(p, limit)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add(encoded[checkpointEnvelopeBytes:])
	f.Add([]byte{})
	f.Add([]byte(`{"Version":1,"CheckpointID":"fuzz","Revision":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > limit {
			return
		}
		_, _ = decodeCheckpoint(data, "", limit)
		if len(data) > limit-checkpointEnvelopeBytes {
			return
		}
		sum := sha256.Sum256(data)
		sealed := make([]byte, 0, checkpointEnvelopeBytes+len(data))
		sealed = append(sealed, "NGSTATE1"...)
		sealed = append(sealed, sum[:]...)
		sealed = append(sealed, data...)
		_, _ = decodeCheckpoint(sealed, "", limit)
	})
}
