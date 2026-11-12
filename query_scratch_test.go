package navmesh_test

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

type pausedSearchContext struct {
	context.Context
	calls   atomic.Int32
	reached chan struct{}
	release chan struct{}
}

func (c *pausedSearchContext) Err() error {
	if c.calls.Add(1) == 2 {
		close(c.reached)
		<-c.release
		return context.Canceled
	}
	return nil
}

func TestQueryScratchRemainsExclusiveThroughCancellation(t *testing.T) {
	_, q := setup(t)
	a, z := project(t, q, -5, 3.1, 0), project(t, q, 5, 3.1, 0)
	ctx := &pausedSearchContext{Context: context.Background(), reached: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	finished := make(chan navmesh.PathResult, 1)
	go func() { finished <- q.FindPath(ctx, a, z, navmesh.Filter{}) }()
	select {
	case <-ctx.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("search did not reach its cancellation boundary")
	}
	if r := q.MoveWithoutVisited(context.Background(), a, a.Position(), navmesh.Filter{}); r.Code != navmesh.InvalidInput {
		t.Fatalf("concurrent scratch use accepted: %+v", r.Status)
	}
	// Sending releases Err without closing the channel so failure cleanup is safe.
	ctx.release <- struct{}{}
	if r := <-finished; r.Code != navmesh.Cancelled {
		t.Fatalf("cancellation lost: %+v", r.Status)
	}
	if r := q.FindPath(context.Background(), a, z, navmesh.Filter{}); r.Code != navmesh.Complete {
		t.Fatalf("cancelled query did not reset for reuse: %+v", r.Status)
	}
}

func TestQueryScratchKeepsResultsIndependent(t *testing.T) {
	s, q := setup(t)
	ctx := context.Background()
	a, z := project(t, q, -5, 3.1, 0), project(t, q, 5, 3.1, 0)
	path := q.FindPath(ctx, a, z, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	corners, tiles, endpoint := path.Path.Corners(), path.Path.Tiles(), path.Path.End()
	move := q.Move(ctx, a, geom.Vec3{X: -4.9, Y: 3.1}, navmesh.Filter{})
	if move.Code != navmesh.Complete || len(move.Visited) == 0 {
		t.Fatal(move)
	}
	visited := append([]navmesh.Surface(nil), move.Visited...)
	if !a.SamePolygon(a) || !move.Surface.SamePolygon(move.Visited[len(move.Visited)-1]) || a.SamePolygon(z) || a.SamePolygon(navmesh.Surface{}) {
		t.Fatal("surface support identity is not exact")
	}
	foreign, _ := setup(t)
	foreignQuery, err := foreign.NewQuery(navmesh.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if a.SamePolygon(project(t, foreignQuery, -5, 3.1, 0)) {
		t.Fatal("equal-looking support from another snapshot has the same identity")
	}
	for i := 0; i < 4; i++ {
		project(t, q, float64(i), .1, 1)
		q.Raycast(ctx, a, z.Position(), navmesh.Filter{})
		q.Move(ctx, z, geom.Vec3{X: 4.9, Y: 3.1}, navmesh.Filter{})
		if r := q.FindPath(ctx, z, a, navmesh.Filter{}); r.Code != navmesh.Complete {
			t.Fatal(r.Status)
		}
		if r := q.RepairPath(ctx, path.Path, move.Surface, navmesh.Filter{}); r.Code != navmesh.Complete {
			t.Fatal(r.Status)
		}
	}
	if !reflect.DeepEqual(corners, path.Path.Corners()) || !reflect.DeepEqual(tiles, path.Path.Tiles()) || endpoint != path.Path.End() || !reflect.DeepEqual(visited, move.Visited) {
		t.Fatal("query scratch escaped into a retained result")
	}
	if path.Path.TileCount() < 2 {
		t.Fatal("fixture must cross multiple tiles")
	}
	small, err := s.NewQuery(navmesh.Limits{PathPolygons: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r := small.RepairPath(ctx, path.Path, a, navmesh.Filter{}); r.Code != navmesh.ResourceExhausted {
		t.Fatalf("oversized corridor was not bounded: %+v", r.Status)
	}
}

func TestMoveWithoutVisitedMatchesMove(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	for _, capacity := range []int{1, 2048} {
		q, err := s.NewQuery(navmesh.Limits{PathPolygons: capacity})
		if err != nil {
			t.Fatal(err)
		}
		a := project(t, q, -5, 3.1, 0)
		for _, target := range []geom.Vec3{{X: -4.9, Y: 3.1}, {X: 5, Y: 3.1}, {X: 20, Y: 3.1}} {
			want := q.Move(ctx, a, target, navmesh.Filter{})
			got := q.MoveWithoutVisited(ctx, a, target, navmesh.Filter{})
			if got.Visited != nil || got.Surface != want.Surface || got.Normal != want.Normal || !reflect.DeepEqual(got.Status, want.Status) {
				t.Fatalf("capacity %d: with visited %+v, without %+v", capacity, want, got)
			}
		}
	}
}

func TestPathAccessorsOwnLinksAndReuseDestinations(t *testing.T) {
	scene, cfg := testworld.Scene()
	scene.Links = []geom.Link{{ID: "stairs", Revision: 1, Start: geom.Vec3{X: -3, Y: .1}, End: geom.Vec3{X: -3, Y: 3.1}, Radius: .4, Area: 2, Flags: 1, Bidirectional: true}}
	built, err := bake.Build(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s, err := navmesh.NewSnapshot(built.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.NewQuery(navmesh.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	a, z := project(t, q, -4, .1, 0), project(t, q, -4, 3.1, 0)
	r := q.FindPath(context.Background(), a, z, navmesh.Filter{})
	if r.Code != navmesh.Complete {
		t.Fatal(r.Status)
	}
	linkStart, linkEnd := project(t, q, -3, .1, 0), project(t, q, -3, 3.1, 0)
	for _, tc := range []struct {
		name       string
		start, end navmesh.Surface
		reverse    bool
	}{
		{"exact-both", linkStart, linkEnd, false},
		{"exact-start", linkStart, z, false},
		{"exact-end", a, linkEnd, false},
		{"exact-reverse", linkEnd, linkStart, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := q.FindPath(context.Background(), tc.start, tc.end, navmesh.Filter{})
			if p.Code != navmesh.Complete {
				t.Fatal(p.Status)
			}
			markers := 0
			for _, c := range p.Path.Corners() {
				if c.Link != nil {
					markers++
					if c.Link.ID != "stairs" || c.Link.Reverse != tc.reverse || c.Position != c.Link.From {
						t.Fatalf("wrong traversal: %+v", c)
					}
				}
			}
			if markers != 1 {
				t.Fatalf("expected one mandatory traversal, found %d", markers)
			}
		})
	}
	small, err := s.NewQuery(navmesh.Limits{Corners: 2})
	if err != nil {
		t.Fatal(err)
	}
	limited := small.FindPath(context.Background(), a, z, navmesh.Filter{})
	if limited.Code != navmesh.Partial || limited.Reason != navmesh.ResourceExhausted {
		t.Fatalf("corner capacity did not truncate: %+v", limited.Status)
	}
	for _, c := range limited.Path.Corners() {
		if c.Link != nil {
			t.Fatal("truncated route exposed a traversal without its landing")
		}
	}
	if h := small.Height(context.Background(), limited.Path.End()); h.Code != navmesh.Complete || h.Height > 1 {
		t.Fatalf("truncated route must end on its entrance floor: %+v", h)
	}
	p := r.Path
	corners := make([]navmesh.Corner, 1, p.CornerCount()+1)
	corners[0].Position.X = 99
	base := &corners[0]
	corners = p.AppendCorners(corners)
	if &corners[0] != base || corners[0].Position.X != 99 || len(corners) != p.CornerCount()+1 {
		t.Fatal("AppendCorners did not preserve/reuse the destination prefix")
	}
	links := 0
	for i := 0; i < p.CornerCount(); i++ {
		value, ok := p.CornerAt(i)
		position, link, hasLink, valid := p.CornerValueAt(i)
		if !ok || !valid || position != value.Position || hasLink != (value.Link != nil) {
			t.Fatal("corner accessors disagree")
		}
		if hasLink {
			links++
			value.Link.ID = "mutated value"
			corners[i+1].Link.ID = "mutated append"
			link.ID = "mutated traversal"
			_, original, _, _ := p.CornerValueAt(i)
			if original.ID != "stairs" {
				t.Fatal("link accessor exposed path-owned memory")
			}
		}
	}
	if links == 0 {
		t.Fatal("fixture must contain a link")
	}
	tiles := make([]navmesh.TileRevision, 1, p.TileCount()+1)
	tiles[0].Revision = 99
	tileBase := &tiles[0]
	tiles = p.AppendTiles(tiles)
	if &tiles[0] != tileBase || tiles[0].Revision != 99 || len(tiles) != p.TileCount()+1 {
		t.Fatal("AppendTiles did not preserve/reuse the destination prefix")
	}
	for i := 0; i < p.TileCount(); i++ {
		v, ok := p.TileAt(i)
		if !ok || v != tiles[i+1] {
			t.Fatal("tile accessors disagree")
		}
	}
	for _, index := range []int{-1, p.CornerCount()} {
		if _, ok := p.CornerAt(index); ok {
			t.Fatal("invalid corner index accepted")
		}
		if _, _, _, ok := p.CornerValueAt(index); ok {
			t.Fatal("invalid corner value index accepted")
		}
	}
	if _, ok := p.TileAt(-1); ok {
		t.Fatal("invalid tile index accepted")
	}
	if _, ok := p.TileAt(p.TileCount()); ok {
		t.Fatal("out-of-bounds tile accepted")
	}
	var nilPath *navmesh.Path
	if nilPath.CornerCount() != 0 || nilPath.TileCount() != 0 || nilPath.Corners() != nil || nilPath.Tiles() != nil {
		t.Fatal("nil path accessor changed")
	}
	if _, ok := nilPath.CornerAt(0); ok {
		t.Fatal("nil corner accepted")
	}
	if _, _, _, ok := nilPath.CornerValueAt(0); ok {
		t.Fatal("nil corner value accepted")
	}
	if _, ok := nilPath.TileAt(0); ok {
		t.Fatal("nil tile accepted")
	}
	t.Run("ValueAllocations", func(t *testing.T) {
		if allocs := testing.AllocsPerRun(100, func() {
			for i := 0; i < p.CornerCount(); i++ {
				p.CornerValueAt(i)
			}
			for i := 0; i < p.TileCount(); i++ {
				p.TileAt(i)
			}
		}); allocs != 0 {
			t.Fatalf("value accessors allocated: %g", allocs)
		}
	})
}

func TestWarmQueryHotPaths(t *testing.T) {
	_, q := setup(t)
	a := project(t, q, -5, 3.1, 0)
	ctx := context.Background()
	for name, call := range map[string]func() navmesh.Status{
		"projection": func() navmesh.Status {
			return q.Project(ctx, geom.Vec3{X: -5, Y: 3.1}, navmesh.ProjectionOptions{Horizontal: .5, Up: .4, Down: .4}, navmesh.Filter{}).Status
		},
		"movement": func() navmesh.Status {
			return q.MoveWithoutVisited(ctx, a, geom.Vec3{X: -4.9, Y: 3.1}, navmesh.Filter{}).Status
		},
		"raycast": func() navmesh.Status {
			return q.Raycast(ctx, a, geom.Vec3{X: 5, Y: 3.1}, navmesh.Filter{}).Status
		},
	} {
		if s := call(); s.Code != navmesh.Complete {
			t.Fatalf("%s: %+v", name, s)
		}
		t.Run(name+"Allocations", func(t *testing.T) {
			if allocs := testing.AllocsPerRun(100, func() {
				if s := call(); s.Code != navmesh.Complete {
					t.Fatal(s)
				}
			}); allocs != 0 {
				t.Fatalf("warm scratch allocations: %g", allocs)
			}
		})
	}
}

func BenchmarkQueryCreation(b *testing.B) {
	s, _ := setup(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.NewQuery(navmesh.Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSurfaceMoveWithoutVisited(b *testing.B) {
	_, q := setup(b)
	a := project(b, q, -5, 3.1, 0)
	ctx := context.Background()
	q.MoveWithoutVisited(ctx, a, geom.Vec3{X: -4.9, Y: 3.1}, navmesh.Filter{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.MoveWithoutVisited(ctx, a, geom.Vec3{X: -4.9, Y: 3.1}, navmesh.Filter{})
	}
}

func BenchmarkRepairPath(b *testing.B) {
	_, q := setup(b)
	a, z := project(b, q, -5, 3.1, 0), project(b, q, 5, 3.1, 0)
	ctx := context.Background()
	p := q.FindPath(ctx, a, z, navmesh.Filter{})
	if p.Code != navmesh.Complete {
		b.Fatal(p.Status)
	}
	q.RepairPath(ctx, p.Path, a, navmesh.Filter{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.RepairPath(ctx, p.Path, a, navmesh.Filter{})
	}
}
