package artifact_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func encoded(t testing.TB) []byte {
	t.Helper()
	scene, c := testworld.Scene()
	r, e := bake.Build(context.Background(), scene, c)
	if e != nil {
		t.Fatal(e)
	}
	b, e := artifact.Encode(r.Bundle)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestEveryTruncationRejected(t *testing.T) {
	data := encoded(t)
	for n := 0; n < len(data); n++ {
		if _, e := artifact.Decode(data[:n], artifact.DefaultLimits()); e == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
	data[20] ^= 1
	if _, e := artifact.Decode(data, artifact.DefaultLimits()); e == nil {
		t.Fatal("accepted checksum corruption")
	}
}
func TestResourceLimitsAndOwnedArrays(t *testing.T) {
	data := encoded(t)
	if _, e := artifact.Decode(data, artifact.Limits{MaxBytes: 32, MaxTiles: 100, MaxElements: 100}); e == nil {
		t.Fatal("bytes limit ignored")
	}
	if _, e := artifact.Decode(data, artifact.Limits{MaxBytes: 1 << 20, MaxTiles: 1, MaxElements: 1 << 20}); e == nil {
		t.Fatal("tile limit ignored")
	}
	a, e := artifact.Decode(data, artifact.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	b, e := a.Clone()
	if e != nil {
		t.Fatal(e)
	}
	for i := range a.Tiles {
		if len(a.Tiles[i].Parameters.Verts) > 0 {
			a.Tiles[i].Parameters.Verts[0] ^= 1
			if a.Tiles[i].Parameters.Verts[0] == b.Tiles[i].Parameters.Verts[0] {
				t.Fatal("clone aliases")
			}
			break
		}
	}
}

func TestGridIdentityAndEmptyPrecision(t *testing.T) {
	b, e := artifact.Decode(encoded(t), artifact.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	for i := range b.Tiles {
		b.Tiles[i].Key.X += 100
		b.Tiles[i].Parameters.TileX += 100
	}
	if e = b.Validate(); e == nil {
		t.Fatal("accepted shifted tile identity with unchanged geometry")
	}
	b, e = artifact.Decode(encoded(t), artifact.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	for i := range b.Tiles {
		if !b.Tiles[i].Empty {
			b.Tiles[i].Parameters.Bmin[0] += .5
			break
		}
	}
	if e = b.Validate(); e == nil {
		t.Fatal("accepted misaligned tile geometry")
	}
	b.Tiles = nil
	b.Settings.Origin.X = 1e20
	if e = b.Validate(); e == nil {
		t.Fatal("empty dataset bypasses precision validation")
	}
}
func FuzzDecode(f *testing.F) {
	f.Add(encoded(f))
	f.Add([]byte("NAVGO"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			return
		}
		data = append([]byte(nil), data...)
		if len(data) >= 40 {
			sum := sha256.Sum256(data[:len(data)-32])
			copy(data[len(data)-32:], sum[:])
		}
		b, e := artifact.Decode(data, artifact.Limits{MaxBytes: 2 << 20, MaxTiles: 1024, MaxElements: 1 << 18})
		if e == nil {
			if _, e = artifact.Encode(b); e != nil {
				t.Fatalf("decoded invalid bundle: %v", e)
			}
		}
	})
}
