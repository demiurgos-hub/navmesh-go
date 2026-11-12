package navmesh

import (
	"context"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func transferQuery(t *testing.T, snapshot *Snapshot, limits Limits) *Query {
	t.Helper()
	q, err := snapshot.NewQuery(limits)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func transferProject(t *testing.T, q *Query, position Vec3) Surface {
	t.Helper()
	r := q.Project(context.Background(), position, ProjectionOptions{Horizontal: .5, Up: .3, Down: .3}, Filter{})
	if r.Code != Complete {
		t.Fatal(r.Status)
	}
	return r.Surface
}

func TestRebaseUnchangedPathPreservesCorridorAndRemapsSalt(t *testing.T) {
	ctx := context.Background()
	for _, differentSalt := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			old, start, goal := scratchChainSnapshot(t, 6, false, partial)
			next, support, _ := scratchChainSnapshot(t, 6, false, partial)
			next.bundle.Revision = 2
			if differentSalt {
				tile := next.mesh.GetTile(0)
				data := append([]byte(nil), tile.Data...)
				if st := next.mesh.RemoveTile(next.mesh.GetTileRef(tile), nil, nil); detour.DtStatusFailed(st) {
					t.Fatal(st)
				}
				if st := next.mesh.AddTile(data, len(data), detour.DT_TILE_FREE_DATA, 0, nil); detour.DtStatusFailed(st) {
					t.Fatal(st)
				}
				support.ref = next.mesh.GetPolyRefBase(next.mesh.GetTile(0))
				if support.ref == start.ref {
					t.Fatal("salt fixture did not change polygon references")
				}
			}
			previous := transferQuery(t, old, Limits{}).FindPath(ctx, start, goal, Filter{})
			if previous.Path == nil || partial && previous.Code != Partial || !partial && previous.Code != Complete {
				t.Fatal(previous.Status)
			}
			q := transferQuery(t, next, Limits{})
			actual := Vec3{X: 1.5, Z: 1}
			got := q.RebaseUnchangedPath(ctx, previous.Path, start, actual, .01, Filter{})
			if got.Path == nil || got.Status != previous.Status || got.Path.status != previous.Path.status || got.Surface.position != actual {
				t.Fatalf("salt=%v partial=%v: %+v", differentSalt, partial, got)
			}
			p := got.Path
			if p == previous.Path || p.snapshot != next || p.start.snapshot != next || p.end.snapshot != next || got.Surface.snapshot != next || p.start.position != start.position || p.end.position != previous.Path.end.position {
				t.Fatal("transfer retained an old snapshot or changed original endpoints")
			}
			if !reflect.DeepEqual(p.Corners(), previous.Path.Corners()) || !reflect.DeepEqual(p.Tiles(), previous.Path.Tiles()) || len(p.refs) != len(previous.Path.refs) {
				t.Fatal("transfer changed immutable corridor geometry")
			}
			for i, ref := range p.refs {
				if ref != support.ref+detour.DtPolyRef(i) || previous.Path.refs[i] != start.ref+detour.DtPolyRef(i) {
					t.Fatal("polygon index/salt mapping changed old refs or returned wrong refs")
				}
			}
			if (&p.refs[0] == &previous.Path.refs[0]) == differentSalt {
				t.Fatal("reference storage must share only when mapped IDs are identical")
			}
			if q.refs != nil || q.points != nil || q.cornRefs != nil || q.seenTiles != nil {
				t.Fatal("unchanged transfer allocated repair/funnel scratch")
			}
			allocationCase := "SameReferences"
			if differentSalt {
				allocationCase = "MappedReferences"
			}
			if partial {
				allocationCase += "Partial"
			}
			t.Run(allocationCase+"Allocations", func(t *testing.T) {
				var retained *Path
				allocations := testing.AllocsPerRun(100, func() {
					retained = q.RebaseUnchangedPath(ctx, previous.Path, start, actual, .01, Filter{}).Path
				})
				want := float64(1) // The independently returned Path header.
				if differentSalt {
					want++ // Its newly mapped polygon references.
				}
				if retained == nil || allocations != want {
					t.Fatalf("got %g allocations; want only %g owned-result allocations", allocations, want)
				}
			})
			corners, tiles := p.Corners(), p.Tiles()
			corners[0].Position.X = 1234
			tiles[0].Revision++
			if p.corners[0].Position.X == 1234 || p.tiles[0].Revision != 1 || previous.Path.tiles[0].Revision != 1 {
				t.Fatal("public views mutated a shared immutable path")
			}
			// The new path must continue working without reading any old mesh.
			old.mesh = nil
			if r := q.RepairPath(ctx, p, got.Surface, Filter{}); r.Path == nil || r.Code != previous.Code {
				t.Fatalf("transferred corridor depended on the old snapshot: %+v", r.Status)
			}
		}
	}
}

func TestRebaseUnchangedPathChecksSupportFiltersAndLimits(t *testing.T) {
	ctx := context.Background()
	old, start, goal := scratchChainSnapshot(t, 4, true, false)
	next, _, _ := scratchChainSnapshot(t, 4, true, false)
	next.bundle.Revision = 2
	old.mesh.GetTile(0).Polys[3].Flags = 2
	next.mesh.GetTile(0).Polys[3].Flags = 2
	p := transferQuery(t, old, Limits{}).FindPath(ctx, start, goal, Filter{}).Path
	if p == nil {
		t.Fatal("missing fixture path")
	}
	for _, tc := range []struct {
		name      string
		limits    Limits
		path      *Path
		support   Surface
		actual    Vec3
		tolerance float64
		filter    Filter
		want      Code
	}{
		{"support-only", Limits{}, nil, start, start.position, .01, Filter{}, Complete},
		{"corridor", Limits{}, p, start, start.position, .01, Filter{}, Complete},
		{"later-support", Limits{}, p, Surface{old, start.ref + 2, Vec3{X: 5, Z: 1}}, Vec3{X: 5, Z: 1}, .01, Filter{}, Complete},
		{"other-floor", Limits{}, p, Surface{old, start.ref + 4, Vec3{X: 1, Y: 6, Z: 1}}, Vec3{X: 1, Y: 6, Z: 1}, .01, Filter{}, Stale},
		{"no-nearest-floor", Limits{}, p, start, Vec3{X: 1, Y: 6, Z: 1}, .1, Filter{}, NoSurface},
		{"support-excluded", Limits{}, p, start, start.position, .01, Filter{IncludeFlags: 2}, NoSurface},
		{"suffix-excluded", Limits{}, p, start, start.position, .01, Filter{IncludeFlags: 1}, Stale},
		{"path-capacity", Limits{PathPolygons: 3}, p, start, start.position, .01, Filter{}, ResourceExhausted},
		{"invalid-tolerance", Limits{}, p, start, start.position, math.NaN(), Filter{}, InvalidInput},
		{"invalid-actual", Limits{}, p, start, Vec3{X: math.Inf(1)}, .01, Filter{}, InvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := transferQuery(t, next, tc.limits)
			r := q.RebaseUnchangedPath(ctx, tc.path, tc.support, tc.actual, tc.tolerance, tc.filter)
			if r.Code != tc.want || tc.want != Complete && r.Path != nil || tc.want == Complete && !r.Surface.Valid() {
				t.Fatalf("got %+v, want %v", r, tc.want)
			}
		})
	}
	q := transferQuery(t, next, Limits{})
	cancelled := &projectionGrowthCancellation{Context: ctx}
	if r := q.RebaseUnchangedPath(cancelled, p, start, start.position, .01, Filter{}); r.Code != Cancelled || r.Path != nil {
		t.Fatalf("corridor transfer ignored cancellation: %+v", r)
	}
	if r := q.RebaseUnchangedPath(ctx, p, start, start.position, .01, Filter{}); r.Code != Complete {
		t.Fatalf("cancelled query could not be reused: %+v", r)
	}
}

func TestRebaseUnchangedPathPreservesAuthoredLinksAndRejectsChanges(t *testing.T) {
	ctx := context.Background()
	scene, cfg := testworld.Scene()
	scene.Links = []geom.Link{{ID: "stairs", Revision: 1, Start: Vec3{X: -3, Y: .1}, End: Vec3{X: -3, Y: 3.1}, Radius: .4, Area: 2, Flags: 1, Bidirectional: true}}
	built, err := bake.Build(ctx, scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	makeSnapshot := func(bundle artifact.Bundle) *Snapshot {
		t.Helper()
		s, err := NewSnapshot(bundle)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	old := makeSnapshot(built.Bundle)
	built.Bundle.Revision++
	next := makeSnapshot(built.Bundle)
	oldQuery, q := transferQuery(t, old, Limits{}), transferQuery(t, next, Limits{})
	a := transferProject(t, oldQuery, Vec3{X: -4, Y: .1})
	z := transferProject(t, oldQuery, Vec3{X: -4, Y: 3.1})
	for _, direction := range [][2]Surface{{a, z}, {z, a}} {
		path := oldQuery.FindPath(ctx, direction[0], direction[1], Filter{})
		if path.Code != Complete || path.Path == nil {
			t.Fatal(path.Status)
		}
		transferred := q.RebaseUnchangedPath(ctx, path.Path, direction[0], direction[0].position, .01, Filter{})
		if transferred.Code != Complete || transferred.Path == nil || !reflect.DeepEqual(transferred.Path.Corners(), path.Path.Corners()) {
			t.Fatalf("traversal geometry changed: %+v", transferred)
		}
		links := 0
		for i, corner := range transferred.Path.Corners() {
			if corner.Link == nil {
				continue
			}
			links++
			original := *path.Path.corners[i].Link
			corner.Link.ID = "caller-mutation"
			if *transferred.Path.corners[i].Link != original || *path.Path.corners[i].Link != original {
				t.Fatal("returned traversal pointer mutated an immutable path")
			}
		}
		if links != 1 {
			t.Fatalf("wanted one mandatory traversal, got %d", links)
		}
		if len(path.Path.corners) < 3 {
			t.Fatal("traversal fixture must exceed the minimum corner capacity")
		}
		limited := transferQuery(t, next, Limits{Corners: 2})
		if r := limited.RebaseUnchangedPath(ctx, path.Path, direction[0], direction[0].position, .01, Filter{}); r.Code != ResourceExhausted || r.Path != nil {
			t.Fatalf("retained corners exceeded the query capacity: %+v", r)
		}
	}
	start := transferProject(t, oldQuery, Vec3{X: -5, Y: 3.1})
	goal := transferProject(t, oldQuery, Vec3{X: 5, Y: 3.1})
	path := oldQuery.FindPath(ctx, start, goal, Filter{}).Path
	if path == nil || len(path.tiles) < 2 {
		t.Fatal("fixture needs a corridor across several tiles")
	}
	supportTile, _ := start.Tile()
	var suffix artifact.TileKey
	for _, tile := range path.tiles {
		if tile.Key != supportTile.Key {
			suffix = tile.Key
			break
		}
	}
	for _, change := range []string{"suffix-revision", "support-removed", "origin", "profile"} {
		t.Run(change, func(t *testing.T) {
			changed := makeSnapshot(built.Bundle)
			switch change {
			case "suffix-revision":
				changed.tiles[suffix]++
			case "support-removed":
				delete(changed.tiles, supportTile.Key)
			case "origin":
				changed.bundle.Settings.Origin.X++
			case "profile":
				changed.bundle.Profile.Height++
			}
			cq := transferQuery(t, changed, Limits{})
			if r := cq.RebaseUnchangedPath(ctx, path, start, start.position, .01, Filter{}); r.Code != Stale || r.Path != nil {
				t.Fatalf("accepted %s: %+v", change, r)
			}
			if change == "suffix-revision" {
				if r := cq.RebasePath(ctx, path, start, start.position, .01, Filter{}); r.Code != Partial || r.Path == nil {
					t.Fatalf("ordinary repairing rebase lost safe partial behavior: %+v", r)
				}
			}
		})
	}
}

type pausedTransferContext struct {
	context.Context
	calls            atomic.Int32
	reached, release chan struct{}
}

func (c *pausedTransferContext) Err() error {
	if c.calls.Add(1) == 2 {
		close(c.reached)
		<-c.release
		return context.Canceled
	}
	return nil
}

func TestRebaseUnchangedPathKeepsQueryExclusive(t *testing.T) {
	old, start, goal := scratchChainSnapshot(t, 4, false, false)
	next, _, _ := scratchChainSnapshot(t, 4, false, false)
	p := transferQuery(t, old, Limits{}).FindPath(context.Background(), start, goal, Filter{}).Path
	q := transferQuery(t, next, Limits{})
	ctx := &pausedTransferContext{Context: context.Background(), reached: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	finished := make(chan RebaseResult, 1)
	go func() { finished <- q.RebaseUnchangedPath(ctx, p, start, start.position, .01, Filter{}) }()
	select {
	case <-ctx.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not reach its cancellation boundary")
	}
	if r := q.RebaseUnchangedPath(context.Background(), p, start, start.position, .01, Filter{}); r.Code != InvalidInput {
		t.Fatalf("query accepted concurrent transfer: %+v", r)
	}
	ctx.release <- struct{}{}
	if r := <-finished; r.Code != Cancelled || r.Path != nil {
		t.Fatalf("transfer lost cancellation: %+v", r)
	}
	if r := q.RebaseUnchangedPath(context.Background(), p, start, start.position, .01, Filter{}); r.Code != Complete || r.Path == nil {
		t.Fatalf("cancelled query remained busy: %+v", r)
	}
}
