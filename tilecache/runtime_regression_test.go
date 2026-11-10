package dtcache

import (
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

type testCompressor struct {
	fail  bool
	short bool
}

func (*testCompressor) MaxCompressedSize(n int32) int32 { return n }
func (*testCompressor) Compress(a []byte, n int32, b []byte, max int32, out *int32) detour.DtStatus {
	if n > max || int(n) > len(a) || int(n) > len(b) {
		return detour.DT_FAILURE
	}
	copy(b, a[:n])
	*out = n
	return detour.DT_SUCCESS
}
func (c *testCompressor) Decompress(a []byte, n int32, b []byte, max int32, out *int32) detour.DtStatus {
	if c.fail || n > max || int(n) > len(a) || int(n) > len(b) {
		return detour.DT_FAILURE
	}
	copy(b, a[:n])
	*out = n
	if c.short {
		*out--
	}
	return detour.DT_SUCCESS
}

type testProcess struct{}

func (testProcess) Process(p *detour.DtNavMeshCreateParams, areas []uint8, flags []uint16) {
	for i := range flags {
		flags[i] = 1
	}
}

func layerData(t testing.TB, c DtTileCacheCompressor, layer int32) []byte {
	t.Helper()
	h := DtTileCacheLayerHeader{Magic: DT_TILECACHE_MAGIC, Version: DT_TILECACHE_VERSION, Tlayer: layer, Width: 8, Height: 8, Maxx: 7, Maxy: 7, Bmax: [3]float32{8, 2, 8}}
	heights, areas, cons := make([]byte, 64), make([]byte, 64), make([]byte, 64)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := x + y*8
			areas[i] = DT_TILECACHE_WALKABLE_AREA
			if x > 0 {
				cons[i] |= 1
			}
			if y < 7 {
				cons[i] |= 2
			}
			if x < 7 {
				cons[i] |= 4
			}
			if y > 0 {
				cons[i] |= 8
			}
		}
	}
	var data []byte
	var n int32
	if s := DtBuildTileCacheLayer(c, &h, heights, areas, cons, &data, &n); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	return data
}

func cacheAndMesh(t testing.TB, c DtTileCacheCompressor) (*DtTileCache, *detour.DtNavMesh, DtCompressedTileRef) {
	t.Helper()
	cache := DtAllocTileCache()
	if s := cache.Init(&DtTileCacheParams{Cs: 1, Ch: 1, Width: 8, Height: 8, WalkableHeight: 2, WalkableRadius: .3, WalkableClimb: .5, MaxSimplificationError: 1, MaxTiles: 16, MaxObstacles: 16}, c, testProcess{}); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	m := detour.DtAllocNavMesh()
	if s := m.Init(&detour.DtNavMeshParams{TileWidth: 8, TileHeight: 8, MaxTiles: 16, MaxPolys: 128}); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	var ref DtCompressedTileRef
	data := layerData(t, c, 0)
	if s := cache.AddTile(data, int32(len(data)), 0, &ref); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	if s := cache.BuildNavMeshTile(ref, m); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	return cache, m, ref
}

func drain(t testing.TB, c *DtTileCache, m *detour.DtNavMesh) {
	t.Helper()
	for i := 0; i < 100; i++ {
		var done bool
		if s := c.Update(0, m, &done); detour.DtStatusFailed(s) {
			t.Fatal(s)
		}
		if done {
			return
		}
	}
	t.Fatal("update failed to converge")
}
func canProject(t testing.TB, m *detour.DtNavMesh, x, z float32) bool {
	t.Helper()
	q := detour.DtAllocNavMeshQuery()
	if s := q.Init(m, 128); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	var ref detour.DtPolyRef
	if s := q.FindNearestPoly([]float32{x, 0, z}, []float32{.1, .1, .1}, detour.DtAllocDtQueryFilter(), &ref, nil); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	return ref != 0
}

func TestLayerDecodeAndFailureRetry(t *testing.T) {
	comp := new(testCompressor)
	cache, m, ref := cacheAndMesh(t, comp)
	before := m.GetTileRefAt(0, 0, 0)
	var ob DtObstacleRef
	if s := cache.AddObstacle([]float32{4, 0, 4}, 1.5, 2, &ob); detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
	comp.fail = true
	var done bool
	if s := cache.Update(0, m, &done); !detour.DtStatusFailed(s) || done || cache.GetObstacleByRef(ob).State != DT_OBSTACLE_PROCESSING || cache.GetObstacleByRef(ob).Npending == 0 {
		t.Fatal(s, done, cache.GetObstacleByRef(ob))
	}
	if m.GetTileRefAt(0, 0, 0) != before {
		t.Fatal("failed rebuild removed committed tile")
	}
	comp.fail = false
	drain(t, cache, m)
	if cache.GetObstacleByRef(ob).State != DT_OBSTACLE_PROCESSED {
		t.Fatal("retry did not finish")
	}
	comp.short = true
	var layer *DtTileCacheLayer
	tile := cache.GetTileByRef(ref)
	if s := DtDecompressTileCacheLayer(comp, tile.Data, tile.DataSize, &layer); !detour.DtStatusFailed(s) || layer != nil {
		t.Fatal("accepted short decompression")
	}
}

func TestOverlappingRemovalAndEmptyTileRestoration(t *testing.T) {
	cache, m, _ := cacheAndMesh(t, new(testCompressor))
	if !canProject(t, m, 4, 4) {
		t.Fatal("baseline missing")
	}
	var first, second DtObstacleRef
	for _, ref := range []*DtObstacleRef{&first, &second} {
		if s := cache.AddBoxObstacle([]float32{2, -1, 2}, []float32{6, 3, 6}, ref); detour.DtStatusFailed(s) {
			t.Fatal(s)
		}
	}
	drain(t, cache, m)
	if canProject(t, m, 4, 4) {
		t.Fatal("blocker failed")
	}
	cache.RemoveObstacle(first)
	drain(t, cache, m)
	if canProject(t, m, 4, 4) {
		t.Fatal("removing overlapping obstacle revealed blocked surface")
	}
	cache.RemoveObstacle(second)
	drain(t, cache, m)
	if !canProject(t, m, 4, 4) {
		t.Fatal("ground was not restored")
	}
	var all DtObstacleRef
	cache.AddBoxObstacle([]float32{-1, -1, -1}, []float32{9, 3, 9}, &all)
	drain(t, cache, m)
	if m.GetTileRefAt(0, 0, 0) != 0 {
		t.Fatal("empty tile not removed")
	}
	cache.RemoveObstacle(all)
	drain(t, cache, m)
	if !canProject(t, m, 4, 4) {
		t.Fatal("empty tile did not restore")
	}
}

func TestInvalidReferencesAndTouchedLimit(t *testing.T) {
	comp := new(testCompressor)
	cache, m, _ := cacheAndMesh(t, comp)
	bad := DtAllocTileCache()
	bad.Init(&DtTileCacheParams{Cs: 1, Ch: 1, Width: 8, Height: 8, MaxTiles: 3, MaxObstacles: 1}, comp, nil)
	if s := bad.BuildNavMeshTile(bad.EncodeTileId(1, 3), m); !detour.DtStatusFailed(s) {
		t.Fatal("accepted index equal max")
	}
	for i := int32(1); i < 9; i++ {
		b := layerData(t, comp, i)
		if s := cache.AddTile(b, int32(len(b)), 0, nil); detour.DtStatusFailed(s) {
			t.Fatal(s)
		}
	}
	var ref DtObstacleRef
	if s := cache.AddObstacle([]float32{4, 0, 4}, 1, 2, &ref); !detour.DtStatusFailed(s) || !detour.DtStatusDetail(s, detour.DT_BUFFER_TOO_SMALL) || ref != 0 {
		t.Fatal("silently truncated touched tiles", s, ref)
	}
	if s := cache.AddTile(nil, 0, 0, nil); !detour.DtStatusFailed(s) {
		t.Fatal(s)
	}
}

func FuzzDecodeLayer(f *testing.F) {
	c := new(testCompressor)
	f.Add(layerData(f, c, 0))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		var layer *DtTileCacheLayer
		_ = DtDecompressTileCacheLayer(c, b, int32(len(b)), &layer)
	})
}
