package navmesh

import (
	"context"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
)

func checkedSurfaceTile(s Surface) (TileRevision, bool) {
	if !s.Valid() || !s.position.Finite() || s.snapshot.mesh == nil {
		return TileRevision{}, false
	}
	var tile *detour.DtMeshTile
	var poly *detour.DtPoly
	if detour.DtStatusFailed(s.snapshot.mesh.GetTileAndPolyByRef(s.ref, &tile, &poly)) {
		return TileRevision{}, false
	}
	key := artifact.TileKey{X: tile.Header.X, Z: tile.Header.Y, Layer: tile.Header.Layer}
	revision, ok := s.snapshot.tiles[key]
	return TileRevision{Key: key, Revision: revision}, ok
}

func compareSurfaceDependency(t *testing.T, surface Surface) {
	t.Helper()
	want, valid := checkedSurfaceTile(surface)
	got, ok := surface.Tile()
	if got != want || ok != valid {
		t.Fatalf("ref=%d: got %v,%v want %v,%v", surface.ref, got, ok, want, valid)
	}
}

func inspectDependencySnapshot(t *testing.T, snapshot *Snapshot) (ground, offmesh, layers, empty int) {
	t.Helper()
	if len(snapshot.dependencies) != int(snapshot.mesh.GetMaxTiles()) {
		t.Fatal("dependency table does not cover runtime slots")
	}
	compareSurfaceDependency(t, Surface{snapshot: snapshot})
	compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: ^detour.DtPolyRef(0)})
	for slot := 0; slot < int(snapshot.mesh.GetMaxTiles()); slot++ {
		tile := snapshot.mesh.GetTile(slot)
		if tile.Header == nil {
			empty++
			if snapshot.dependencies[slot].count != 0 {
				t.Fatal("unused runtime slot contains cached polygons")
			}
			compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: snapshot.mesh.EncodePolyId(tile.Salt, uint32(slot), 0)})
			continue
		}
		if tile.Header.Layer != 0 {
			layers++
		}
		base := snapshot.mesh.GetPolyRefBase(tile)
		entry := snapshot.dependencies[slot]
		if entry.first != base || entry.count != uint32(tile.Header.PolyCount) {
			t.Fatal("dependency range differs from actual runtime tile")
		}
		for polygon := range tile.Polys {
			ref := base + detour.DtPolyRef(polygon)
			if tile.Polys[polygon].GetType() == detour.DT_POLYTYPE_GROUND {
				ground++
			} else {
				offmesh++
			}
			compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: ref})
			compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: ref, position: Vec3{X: math.NaN()}})
			wrongSalt := snapshot.mesh.EncodePolyId(tile.Salt+1, uint32(slot), uint32(polygon))
			compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: wrongSalt})
		}
		compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: snapshot.mesh.EncodePolyId(tile.Salt, uint32(slot), uint32(tile.Header.PolyCount))})
		compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: snapshot.mesh.EncodePolyId(0, uint32(slot), 0)})
	}
	compareSurfaceDependency(t, Surface{snapshot: snapshot, ref: snapshot.mesh.EncodePolyId(1, uint32(snapshot.mesh.GetMaxTiles()), 0)})
	return
}

func TestSurfaceDependencyCacheMatchesCheckedReferences(t *testing.T) {
	base, update, _ := columnUpdateFixture(t)
	bundle, err := base.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	extra := update.Tiles[0]
	extra.Key.Layer, extra.Parameters.TileLayer = 1, 1
	extra.Parameters.OffMeshConCount = 0
	extra.Parameters.OffMeshConVerts, extra.Parameters.OffMeshConRad = nil, nil
	extra.Parameters.OffMeshConFlags, extra.Parameters.OffMeshConAreas = nil, nil
	extra.Parameters.OffMeshConDir, extra.Parameters.OffMeshConUserID = nil, nil
	bundle.Tiles = append(bundle.Tiles, extra)
	for i := range bundle.Tiles {
		if !bundle.Tiles[i].Empty && bundle.Tiles[i].Parameters.OffMeshConCount == 0 && bundle.Tiles[i].Key != extra.Key {
			bundle.Tiles[i] = artifact.Tile{Key: bundle.Tiles[i].Key, Revision: bundle.Tiles[i].Revision, Empty: true}
			break
		}
	}
	snapshot, err := NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	ground, offmesh, layers, empty := inspectDependencySnapshot(t, snapshot)
	if ground == 0 || offmesh == 0 || layers == 0 || empty == 0 {
		t.Fatalf("missing dependency coverage: ground=%d offmesh=%d layers=%d empty=%d", ground, offmesh, layers, empty)
	}
	// The optimization changes only snapshot metadata, not every live handle.
	oldLayout := struct {
		snapshot *Snapshot
		ref      detour.DtPolyRef
		position Vec3
	}{}
	if reflect.TypeOf(Surface{}).Size() != reflect.TypeOf(oldLayout).Size() {
		t.Fatal("tile dependencies increased Surface size")
	}
}

func TestSurfaceDependencyCacheFallbackAndStaleSalt(t *testing.T) {
	raw, start, _ := scratchChainSnapshot(t, 3, false, false)
	if len(raw.dependencies) != 0 {
		t.Fatal("raw fixture unexpectedly has assembled cache")
	}
	compareSurfaceDependency(t, start)
	tile := raw.mesh.GetTile(0)
	data := append([]byte(nil), tile.Data...)
	if st := raw.mesh.RemoveTile(raw.mesh.GetTileRef(tile), nil, nil); detour.DtStatusFailed(st) {
		t.Fatal(st)
	}
	compareSurfaceDependency(t, start)
	if _, ok := start.Tile(); ok {
		t.Fatal("removed raw tile retained a dependency")
	}
	if st := raw.mesh.AddTile(data, len(data), detour.DT_TILE_FREE_DATA, 0, nil); detour.DtStatusFailed(st) {
		t.Fatal(st)
	}
	compareSurfaceDependency(t, start)
	if _, ok := start.Tile(); ok {
		t.Fatal("stale raw salt retained a dependency")
	}
	start.ref = raw.mesh.GetPolyRefBase(raw.mesh.GetTile(0))
	compareSurfaceDependency(t, start)
	base, _, _ := columnUpdateFixture(t)
	ref := base.mesh.GetPolyRefBase(base.mesh.GetTile(0))
	withoutCache := *base
	withoutCache.dependencies = nil
	compareSurfaceDependency(t, Surface{snapshot: &withoutCache, ref: ref})
	miss := *base
	miss.dependencies = append([]surfaceTileDependency(nil), base.dependencies...)
	miss.dependencies[0] = surfaceTileDependency{}
	compareSurfaceDependency(t, Surface{snapshot: &miss, ref: ref})
	compareSurfaceDependency(t, Surface{})
	compareSurfaceDependency(t, Surface{snapshot: &Snapshot{}, ref: 1})
}

func TestSurfaceDependencyCacheAcrossMovementAndRebuild(t *testing.T) {
	base, _, _ := columnUpdateFixture(t)
	query := transferQuery(t, base, Limits{})
	start := transferProject(t, query, Vec3{X: -3, Y: 3.1})
	goal := transferProject(t, query, Vec3{X: 5, Y: 3.1})
	path := query.FindPath(context.Background(), start, goal, Filter{})
	if path.Code != Complete {
		t.Fatal(path.Status)
	}
	compareSurfaceDependency(t, start)
	compareSurfaceDependency(t, path.Path.End())
	for _, target := range []Vec3{{X: -2.9, Y: 3.1}, {X: 3, Y: 3.1}} {
		moved := query.Move(context.Background(), start, target, Filter{})
		if moved.Code != Complete {
			t.Fatal(moved.Status)
		}
		compareSurfaceDependency(t, moved.Surface)
		for _, visited := range moved.Visited {
			compareSurfaceDependency(t, visited)
		}
	}
	metadata, err := base.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	metadata.Revision++
	metadata.Tiles = nil
	first := base.mesh.GetTile(0)
	remove := artifact.TileKey{X: first.Header.X, Z: first.Header.Y}
	if first.Header.OffMeshConCount != 0 {
		t.Fatal("first-slot removal must not remove authored link metadata")
	}
	next, err := base.RebuildColumns(metadata, nil, []artifact.TileKey{remove})
	if err != nil {
		t.Fatal(err)
	}
	inspectDependencySnapshot(t, next)
	q := transferQuery(t, next, Limits{})
	transferred := q.RebaseUnchangedPath(context.Background(), path.Path, start, start.position, .01, Filter{})
	if transferred.Code != Complete || transferred.Path == nil {
		t.Fatal(transferred.Status)
	}
	if base.mesh.DecodePolyIdTile(start.ref) == next.mesh.DecodePolyIdTile(transferred.Surface.ref) {
		t.Fatal("rebuild fixture did not change runtime slot assignment")
	}
	want, _ := start.Tile()
	got, ok := transferred.Surface.Tile()
	if !ok || got != want {
		t.Fatal("slot remapping changed tile dependency")
	}
	compareSurfaceDependency(t, transferred.Surface)
	compareSurfaceDependency(t, transferred.Path.End())
	if !next.Topology().SurfaceUnchanged(start) {
		t.Fatal("unchanged foreign support was rejected")
	}
	foreign, _ := base.Bundle()
	for i := range foreign.Tiles {
		if foreign.Tiles[i].Key == want.Key {
			foreign.Tiles[i].Revision++
		}
	}
	changed, err := NewSnapshot(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Topology().SurfaceUnchanged(start) {
		t.Fatal("cached support bypassed foreign revision validation")
	}
	got.Revision++
	if again, _ := transferred.Surface.Tile(); again != want {
		t.Fatal("returned dependency value aliases immutable metadata")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				if tile, ok := transferred.Surface.Tile(); !ok || tile != want {
					t.Error("concurrent dependency read changed")
				}
			}
		}()
	}
	wg.Wait()
	t.Run("DependencyAllocations", func(t *testing.T) {
		if allocations := testing.AllocsPerRun(100, func() {
			if tile, ok := transferred.Surface.Tile(); !ok || tile != want {
				panic("dependency changed")
			}
		}); allocations != 0 {
			t.Fatalf("dependency cache allocated %g times", allocations)
		}
	})
}
