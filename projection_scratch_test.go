package navmesh

import (
	"context"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// A connected chain gives candidate counts and path lengths independent of
// Recast simplification. The optional upper chain is a distinct stacked floor.
func scratchChainSnapshot(t *testing.T, count int, stacked, disconnected bool) (*Snapshot, Surface, Surface) {
	t.Helper()
	floors := 1
	if stacked {
		floors = 2
	}
	total := count * floors
	p := detour.DtNavMeshCreateParams{Nvp: 6, PolyCount: int32(total), VertCount: int32(total * 4), Cs: 1, Ch: 1, WalkableHeight: 2, WalkableRadius: .3, WalkableClimb: .5, Bmax: [3]float32{float32(count * 2), 8, 2}}
	p.PolyFlags, p.PolyAreas, p.Polys = make([]uint16, total), make([]uint8, total), make([]uint16, total*12)
	for i := range p.Polys {
		p.Polys[i] = 65535
	}
	for i := 0; i < total; i++ {
		x, y, local := uint16((i%count)*2), uint16((i/count)*6), i%count
		p.Verts = append(p.Verts, x, y, 0, x, y, 2, x+2, y, 2, x+2, y, 0)
		p.PolyFlags[i] = 1
		for j := 0; j < 4; j++ {
			p.Polys[i*12+j] = uint16(i*4 + j)
		}
		if local > 0 && !(disconnected && local == count-1) {
			p.Polys[i*12+6] = uint16(i - 1)
		}
		if local < count-1 && !(disconnected && local == count-2) {
			p.Polys[i*12+8] = uint16(i + 1)
		}
	}
	var data []byte
	var size int
	if !detour.DtCreateNavMeshData(&p, &data, &size) {
		t.Fatal("chain tile construction failed")
	}
	mesh := detour.DtAllocNavMesh()
	if st := mesh.Init2(data, size, detour.DT_TILE_FREE_DATA); detour.DtStatusFailed(st) {
		t.Fatal(st)
	}
	snapshot := &Snapshot{mesh: mesh, tiles: map[artifact.TileKey]uint64{{}: 1}, bundle: artifact.Bundle{Revision: 1, Settings: artifact.Settings{CellSize: 1, TileSize: int32(count * 2), Bounds: geom.Bounds{Min: Vec3{Y: -1}, Max: Vec3{X: float64(count * 2), Y: 8, Z: 2}}}}}
	ref := mesh.GetPolyRefBase(mesh.GetTile(0))
	return snapshot, Surface{snapshot, ref, Vec3{X: 1, Z: 1}}, Surface{snapshot, ref + detour.DtPolyRef(count-1), Vec3{X: float64(count*2 - 1), Z: 1}}
}

func TestProjectionScratchGrowsWithoutTruncatingSelection(t *testing.T) {
	ctx := context.Background()
	snapshot, start, goal := scratchChainSnapshot(t, 70, false, false)
	wide := ProjectionOptions{Horizontal: 200, Up: 1, Down: 1}
	reference, _ := snapshot.NewQuery(Limits{})
	reference.refs = make([]detour.DtPolyRef, reference.limits.Candidates)
	want := reference.Project(ctx, goal.position, wide, Filter{})
	if want.Code != Complete || want.Surface != goal {
		t.Fatal(want)
	}
	for _, limit := range []int{31, 32, 69, 70, 71, 4096} {
		q, _ := snapshot.NewQuery(Limits{Candidates: limit})
		got := q.Project(ctx, goal.position, wide, Filter{})
		if limit < 70 {
			if got.Code != ResourceExhausted || got.Surface.Valid() {
				t.Fatalf("limit=%d selected a truncated candidate set: %+v", limit, got)
			}
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("limit=%d got %+v want %+v", limit, got, want)
		}
		if cap(q.refs) > limit || limit == 4096 && cap(q.refs) != 128 {
			t.Fatalf("limit=%d grew scratch to %d", limit, cap(q.refs))
		}
	}
	q, _ := snapshot.NewQuery(Limits{})
	narrow := ProjectionOptions{Horizontal: .1, Up: 1, Down: 1}
	retained := q.Project(ctx, start.position, narrow, Filter{})
	if retained.Code != Complete || retained.Surface != start || cap(q.refs) != 32 {
		t.Fatalf("small local query allocated maximum scratch: %+v cap=%d", retained, cap(q.refs))
	}
	if got := q.Project(ctx, goal.position, wide, Filter{}); got.Surface != goal || got.Code != Complete || retained.Surface != start {
		t.Fatal("candidate growth changed retained support or selection")
	}
	t.Run("ReuseAllocations", func(t *testing.T) {
		if allocations := testing.AllocsPerRun(100, func() { q.Project(ctx, goal.position, wide, Filter{}) }); allocations != 0 {
			t.Fatalf("grown candidate storage was not reused: %g", allocations)
		}
	})
	hinted, _ := snapshot.NewQuery(Limits{})
	narrow.Hint = start
	if got := hinted.Project(ctx, start.position, narrow, Filter{}); got.Code != Complete || len(hinted.refs) != 0 {
		t.Fatalf("successful hint allocated candidates: %+v", got)
	}
}

func TestProjectionGrowthPreservesStackedFloorAmbiguity(t *testing.T) {
	snapshot, _, goal := scratchChainSnapshot(t, 70, true, false)
	point := goal.position
	point.Y = 3
	options := ProjectionOptions{Horizontal: 200, Up: 4, Down: 4}
	for _, limit := range []int{139, 140, 4096} {
		q, _ := snapshot.NewQuery(Limits{Candidates: limit})
		got := q.Project(context.Background(), point, options, Filter{})
		want := Ambiguous
		if limit < 140 {
			want = ResourceExhausted
		}
		if got.Code != want || got.Surface.Valid() {
			t.Fatalf("limit=%d: %+v", limit, got)
		}
	}
}

type projectionGrowthCancellation struct {
	context.Context
	checks int
}

func (c *projectionGrowthCancellation) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}
	return nil
}

func TestProjectionCanCancelBetweenGrowthPasses(t *testing.T) {
	snapshot, _, goal := scratchChainSnapshot(t, 70, false, false)
	q, _ := snapshot.NewQuery(Limits{})
	ctx := &projectionGrowthCancellation{Context: context.Background()}
	options := ProjectionOptions{Horizontal: 200, Up: 1, Down: 1}
	if got := q.Project(ctx, goal.position, options, Filter{}); got.Code != Cancelled || cap(q.refs) != 32 {
		t.Fatalf("did not cancel before candidate growth: %+v cap=%d", got, cap(q.refs))
	}
	if got := q.Project(context.Background(), goal.position, options, Filter{}); got.Code != Complete || got.Surface != goal {
		t.Fatalf("cancelled query was not reusable: %+v", got)
	}
}

func TestPathScratchBoundedByVisitedNodes(t *testing.T) {
	ctx := context.Background()
	for _, disconnected := range []bool{false, true} {
		snapshot, start, goal := scratchChainSnapshot(t, 70, false, disconnected)
		for _, limits := range []Limits{{}, {PathPolygons: 17}, {Nodes: 4}} {
			q, _ := snapshot.NewQuery(limits)
			got := q.FindPath(ctx, start, goal, Filter{})
			if got.Path == nil || got.Path.refs[0] != start.ref || len(got.Path.refs) > len(q.refs) || len(q.refs) > int(q.query.GetNodePool().GetNodeCount()) {
				t.Fatalf("invalid path/scratch prefix: %+v refs=%d", got.Status, len(q.refs))
			}
			wantCode, wantReason, wantCount := Complete, Complete, 70
			if disconnected {
				wantCode, wantReason, wantCount = Partial, Unreachable, 69
			}
			if limits.PathPolygons != 0 {
				wantCode, wantReason, wantCount = Partial, ResourceExhausted, limits.PathPolygons
			}
			if limits.Nodes != 0 {
				wantCode, wantReason, wantCount = Partial, ResourceExhausted, limits.Nodes
			}
			if got.Code != wantCode || got.Reason != wantReason || len(got.Path.refs) != wantCount || cap(q.refs) != wantCount {
				t.Fatalf("disconnected=%t limits=%+v: status=%+v path=%d scratch=%d", disconnected, limits, got.Status, len(got.Path.refs), cap(q.refs))
			}
			retained := append([]detour.DtPolyRef(nil), got.Path.refs...)
			// The same-polygon Detour shortcut leaves the previous node count.
			// Extraction still needs exactly one element, including on reuse.
			same := q.FindPath(ctx, start, start, Filter{})
			if same.Code != Complete || len(q.refs) != 1 || len(same.Path.refs) != 1 || !reflect.DeepEqual(got.Path.refs, retained) {
				t.Fatal("same-polygon search reused a stale count or changed retained path")
			}
		}
		fresh, _ := snapshot.NewQuery(Limits{})
		if got := fresh.FindPath(ctx, start, start, Filter{}); got.Code != Complete || cap(fresh.refs) != 1 {
			t.Fatalf("fresh same-polygon path scratch=%d status=%+v", cap(fresh.refs), got.Status)
		}
	}
}
