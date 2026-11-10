package loader

import (
	"os"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

func sample(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile("../examples/assets/navmesh.bin")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeTileMeshAndRejectedPartialLoads(t *testing.T) {
	b := sample(t)
	m, err := DecodeTileMesh(b)
	if err != nil || m == nil {
		t.Fatal(err)
	}
	loaded := 0
	for i := 0; i < int(m.GetMaxTiles()); i++ {
		if m.GetTile(i).Header != nil {
			loaded++
		}
	}
	if loaded == 0 {
		t.Fatal("sample did not load any tiles")
	}
	for _, n := range []int{0, 1, 8, 39, 40, 48, 100, len(b) / 2, len(b) - 1} {
		if mesh, err := DecodeTileMesh(b[:n]); err == nil || mesh != nil {
			t.Fatalf("accepted truncated length %d", n)
		}
	}
	bad := append([]byte(nil), b...)
	for i := 48; i < 52; i++ {
		bad[i] = 0
	}
	if mesh, err := DecodeTileMesh(bad); err == nil || mesh != nil {
		t.Fatal("returned partially loaded mesh")
	}
	if LoadTileMeshByBytes(nil) != nil {
		t.Fatal("legacy invalid input")
	}
	if _, err := ReadTileMesh("testdata/does-not-exist.bin"); err == nil {
		t.Fatal("lost I/O error")
	}
	if LoadTileMesh("testdata/does-not-exist.bin") != nil {
		t.Fatal("legacy missing file")
	}
}

func TestCompressionWrapperRejectsFailureAndCapacityMismatch(t *testing.T) {
	c := new(FastLZCompressor)
	var n int32
	if s := c.Decompress([]byte{255}, 1, make([]byte, 32), 32, &n); !detour.DtStatusFailed(s) || n != 0 {
		t.Fatal(s, n)
	}
	if s := c.Compress([]byte{1}, 2, make([]byte, 100), 100, &n); !detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	if s := c.Decompress(nil, 0, nil, 0, nil); !detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
}

func TestTempObstacleLoaderRejectsShortInput(t *testing.T) {
	for n := 0; n < 100; n++ {
		b := make([]byte, n)
		if m, c, err := DecodeTempObstacles(b); m != nil || c != nil || err == nil {
			t.Fatal(n)
		}
	}
}

func FuzzDecodeLegacyMeshSets(f *testing.F) {
	f.Add(sample(f))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		_, _ = DecodeTileMesh(b)
		_, _, _ = DecodeTempObstacles(b)
	})
}
