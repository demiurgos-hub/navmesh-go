package navmesh

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

// Exercise the full Detour traversal independently of the facade shortcut.
func referenceMovement(t *testing.T, q *Query, start Surface, target Vec3, f Filter) MoveResult {
	t.Helper()
	filter, err := q.detourFilter(f)
	if err != nil {
		t.Fatal(err)
	}
	a, b := q.local(start.position), q.local(target)
	refs := make([]detour.DtPolyRef, q.localMovementLimit())
	var pos [3]float32
	var count int
	var hit bool
	st := q.query.MoveAlongSurface(start.ref, a[:], b[:], filter, pos[:], refs, &count, len(refs), &hit)
	if detour.DtStatusFailed(st) || count == 0 {
		t.Fatalf("reference traversal failed: %#x, count %d", st, count)
	}
	ref := refs[count-1]
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL) && detour.DtStatusFailed(q.query.ClosestPointOnPoly(ref, pos[:], pos[:], nil)) {
		t.Fatal("reference truncation clamp failed")
	}
	if detour.DtStatusFailed(q.query.GetPolyHeight(ref, pos[:], &pos[1])) {
		t.Fatal("reference height failed")
	}
	out := MoveResult{Surface: Surface{q.snapshot, ref, q.world(pos)}, Normal: q.normal(ref, pos)}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL|detour.DT_OUT_OF_NODES) {
		out.Status = Status{Code: Partial, Reason: ResourceExhausted}
	}
	for _, ref := range refs[:count] {
		var near [3]float32
		if !detour.DtStatusFailed(q.query.ClosestPointOnPoly(ref, pos[:], near[:], nil)) {
			out.Visited = append(out.Visited, Surface{q.snapshot, ref, q.world(near)})
		}
	}
	return out
}

func interiorMovementFixture(t *testing.T, terrain bool) (*Snapshot, Surface, *detour.DtMeshTile, *detour.DtPoly) {
	t.Helper()
	var bundle artifact.Bundle
	if terrain {
		data, err := os.ReadFile("testdata/movement-detail-height.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &bundle); err != nil {
			t.Fatal(err)
		}
	} else {
		scene, cfg := testworld.Scene()
		built, err := bake.Build(context.Background(), scene, cfg)
		if err != nil {
			t.Fatal(err)
		}
		bundle = built.Bundle
	}
	snapshot, err := NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := snapshot.NewQuery(Limits{})
	for i := 0; i < int(snapshot.mesh.GetMaxTiles()); i++ {
		tile := snapshot.mesh.GetTile(i)
		if tile == nil || tile.Header == nil {
			continue
		}
		for j := range tile.Polys {
			poly := &tile.Polys[j]
			if poly.GetType() != detour.DT_POLYTYPE_GROUND {
				continue
			}
			var center [3]float32
			for _, index := range poly.Verts[:poly.VertCount] {
				for k := range center {
					center[k] += tile.Verts[int(index)*3+k] / float32(poly.VertCount)
				}
			}
			if !strictlyInsideGroundPolygon(tile, poly, center) {
				continue
			}
			ref := snapshot.mesh.GetPolyRefBase(tile) | detour.DtPolyRef(j)
			if detour.DtStatusFailed(q.query.GetPolyHeight(ref, center[:], &center[1])) {
				continue
			}
			return snapshot, Surface{snapshot, ref, q.world(center)}, tile, poly
		}
	}
	t.Fatal("fixture has no interior ground support")
	return nil, Surface{}, nil, nil
}

func TestInteriorMovementMatchesFullTraversal(t *testing.T) {
	ctx := context.Background()
	for _, terrain := range []bool{false, true} {
		snapshot, start, tile, poly := interiorMovementFixture(t, terrain)
		v0 := tile.Verts[int(poly.Verts[0])*3:]
		v1 := tile.Verts[int(poly.Verts[1])*3:]
		center := start.position
		o := snapshot.bundle.Settings.Origin
		vertex := Vec3{X: float64(v0[0]) + o.X, Y: float64(v0[1]) + o.Y, Z: float64(v0[2]) + o.Z}
		targets := []Vec3{
			center,
			{X: center.X*.9 + vertex.X*.1, Y: center.Y + 1000, Z: center.Z*.9 + vertex.Z*.1},
			vertex,
			{X: float64((v0[0]+v1[0])*.5) + o.X, Y: center.Y, Z: float64((v0[2]+v1[2])*.5) + o.Z},
			{X: center.X + (vertex.X-center.X)*1.25, Y: center.Y, Z: center.Z + (vertex.Z-center.Z)*1.25},
		}
		for _, capacity := range []int{1, 2, 2048} {
			q, _ := snapshot.NewQuery(Limits{PathPolygons: capacity})
			reference, _ := snapshot.NewQuery(Limits{PathPolygons: capacity})
			for i, target := range targets {
				want := referenceMovement(t, reference, start, target, Filter{})
				got := q.Move(ctx, start, target, Filter{})
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("terrain=%t capacity=%d target=%d: got %+v, want %+v", terrain, capacity, i, got, want)
				}
				without := q.MoveWithoutVisited(ctx, start, target, Filter{})
				want.Visited = nil
				if !reflect.DeepEqual(without, want) {
					t.Fatalf("without visited terrain=%t capacity=%d target=%d: got %+v, want %+v", terrain, capacity, i, without, want)
				}
				if i == 0 && capacity == 1 && (got.Code != Partial || got.Reason != ResourceExhausted) {
					t.Fatalf("one-slot truncation status changed: %+v", got.Status)
				}
			}
		}
		q, _ := snapshot.NewQuery(Limits{})
		first := q.Move(ctx, start, targets[0], Filter{})
		retained := append([]Surface(nil), first.Visited...)
		second := q.Move(ctx, start, targets[1], Filter{})
		second.Visited[0] = Surface{}
		if !reflect.DeepEqual(first.Visited, retained) {
			t.Fatal("interior visited results share mutable storage")
		}
		if len(q.refs) != 0 || q.query.GetNodePool() != nil {
			t.Fatal("interior movement allocated traversal scratch")
		}
	}
}

func TestInteriorMovementChecksAndColdScratch(t *testing.T) {
	snapshot, start, _, _ := interiorMovementFixture(t, false)
	ctx := context.Background()
	q, _ := snapshot.NewQuery(Limits{})
	badCost := Filter{}
	badCost.AreaCosts[63] = float32(math.NaN())
	for _, tc := range []struct {
		filter Filter
		code   Code
	}{{Filter{ExcludeFlags: 65535}, NoSurface}, {Filter{IncludeFlags: 0x8000}, NoSurface}, {badCost, InvalidInput}, {Filter{}, Complete}} {
		if got := q.MoveWithoutVisited(ctx, start, start.position, tc.filter); got.Code != tc.code {
			t.Fatalf("filter %+v returned %+v", tc.filter, got.Status)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got := q.MoveWithoutVisited(cancelled, start, start.position, Filter{}); got.Code != Cancelled {
		t.Fatal(got.Status)
	}
	foreign, _, _, _ := interiorMovementFixture(t, false)
	stale := start
	stale.snapshot = foreign
	if got := q.MoveWithoutVisited(ctx, stale, start.position, Filter{}); got.Code != Stale {
		t.Fatal(got.Status)
	}
	if got := q.MoveWithoutVisited(ctx, start, Vec3{X: math.Inf(1)}, Filter{}); got.Code != InvalidInput {
		t.Fatal(got.Status)
	}
	t.Run("ColdAllocations", func(t *testing.T) {
		// AllocsPerRun includes one warmup call. Every call gets a fresh query,
		// so zero allocations proves no tiny pool or refs are created either.
		queries := make([]*Query, 101)
		for i := range queries {
			queries[i], _ = snapshot.NewQuery(Limits{})
		}
		index := 0
		var result MoveResult
		allocations := testing.AllocsPerRun(100, func() {
			result = queries[index].MoveWithoutVisited(ctx, start, start.position, Filter{})
			index++
		})
		if result.Code != Complete || allocations != 0 {
			t.Fatalf("cold interior movement: status=%+v allocations=%g", result.Status, allocations)
		}
		for _, query := range queries {
			if len(query.refs) != 0 || query.query.GetNodePool() != nil {
				t.Fatal("cold interior move retained traversal scratch")
			}
		}
	})
}

func TestStrictInteriorRejectsBoundaryAndOffMesh(t *testing.T) {
	tile := &detour.DtMeshTile{Verts: []float32{0, 0, 0, 2, 0, 0, 2, 0, 2, 0, 0, 2}}
	poly := &detour.DtPoly{VertCount: 4}
	copy(poly.Verts[:], []uint16{0, 1, 2, 3})
	for _, order := range [][]uint16{{0, 1, 2, 3}, {3, 2, 1, 0}} {
		copy(poly.Verts[:], order)
		for _, tc := range []struct {
			point  [3]float32
			inside bool
		}{{[3]float32{1, 100, 1}, true}, {[3]float32{0, 0, 1}, false}, {[3]float32{2, 0, 1}, false}, {[3]float32{1, 0, 0}, false}, {[3]float32{1, 0, 2}, false}, {[3]float32{0, 0, 0}, false}, {[3]float32{3, 0, 1}, false}} {
			if got := strictlyInsideGroundPolygon(tile, poly, tc.point); got != tc.inside {
				t.Fatalf("order %v point %v: inside=%t", order, tc.point, got)
			}
		}
	}
	poly.SetType(detour.DT_POLYTYPE_OFFMESH_CONNECTION)
	if strictlyInsideGroundPolygon(tile, poly, [3]float32{1, 0, 1}) {
		t.Fatal("off-mesh polygon entered the ground shortcut")
	}
}

func TestDefaultFilterResetsCustomAndInvalidCosts(t *testing.T) {
	var q Query
	custom := Filter{IncludeFlags: 4, ExcludeFlags: 2}
	custom.AreaCosts[0], custom.AreaCosts[63] = 7, 3
	invalid := custom
	invalid.AreaCosts[63] = float32(math.Inf(1))
	negativeZero := Filter{}
	negativeZero.AreaCosts[2] = float32(math.Copysign(0, -1))
	for _, input := range []Filter{{}, custom, {}, invalid, {IncludeFlags: 8, ExcludeFlags: 16}, negativeZero, custom, {}} {
		filter, err := q.detourFilter(input)
		if math.IsInf(float64(input.AreaCosts[63]), 0) {
			if err == nil {
				t.Fatal("invalid custom cost accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		include := input.IncludeFlags
		if include == 0 {
			include = 65535
		}
		if filter.GetIncludeFlags() != include || filter.GetExcludeFlags() != input.ExcludeFlags {
			t.Fatal("cost reset changed filtering flags")
		}
		for i, want := range input.AreaCosts {
			if want == 0 {
				want = 1
			}
			if got := filter.GetAreaCost(i); got != want {
				t.Fatalf("area %d cost=%g want=%g after input %+v", i, got, want, input)
			}
		}
	}
}
