package navmesh_test

import (
	"context"
	"math"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func setup(t testing.TB) (*navmesh.Snapshot, *navmesh.Query) {
	t.Helper()
	scene, c := testworld.Scene()
	r, e := bake.Build(context.Background(), scene, c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := navmesh.NewSnapshot(r.Bundle)
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.NewQuery(navmesh.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	return s, q
}
func project(t testing.TB, q *navmesh.Query, x, y, z float64) navmesh.Surface {
	t.Helper()
	r := q.Project(context.Background(), geom.Vec3{X: x, Y: y, Z: z}, navmesh.ProjectionOptions{Horizontal: .5, Up: .4, Down: .4}, navmesh.Filter{})
	if r.Code != navmesh.Complete {
		t.Fatalf("project %v", r.Status)
	}
	return r.Surface
}
func TestSafeFacadeAndFloorSelection(t *testing.T) {
	var zero navmesh.Query
	if zero.Project(context.Background(), geom.Vec3{}, navmesh.ProjectionOptions{}, navmesh.Filter{}).Code != navmesh.InvalidInput {
		t.Fatal("zero query accepted")
	}
	_, q := setup(t)
	p := project(t, q, 0, .1, 0)
	upper := project(t, q, 0, 3.1, 0)
	r := q.Project(context.Background(), geom.Vec3{Y: 1.6}, navmesh.ProjectionOptions{Horizontal: .5, Up: 2, Down: 2}, navmesh.Filter{})
	if r.Code != navmesh.Ambiguous {
		t.Fatalf("floor tie: %+v", r)
	}
	r = q.Project(context.Background(), geom.Vec3{Y: 1.6}, navmesh.ProjectionOptions{Horizontal: .5, Up: 2, Down: 2, Hint: upper}, navmesh.Filter{})
	if r.Code != navmesh.Complete || r.Surface.Position().Y < 3 {
		t.Fatal("floor hint ignored")
	}
	_, other := setup(t)
	if other.Height(context.Background(), p).Code != navmesh.Stale {
		t.Fatal("foreign handle accepted")
	}
	cost := navmesh.Filter{}
	cost.AreaCosts[1] = .1
	if q.Move(context.Background(), p, p.Position(), cost).Code != navmesh.InvalidInput {
		t.Fatal("inadmissible cost accepted")
	}
	if q.Project(context.Background(), geom.Vec3{X: 1e300}, navmesh.ProjectionOptions{Horizontal: 1}, navmesh.Filter{}).Code != navmesh.InvalidInput {
		t.Fatal("float32 overflow accepted")
	}
	if q.Project(context.Background(), geom.Vec3{Y: .1}, navmesh.ProjectionOptions{Horizontal: 1e200, Up: 1e200, Down: 1e200}, navmesh.Filter{}).Code != navmesh.Complete {
		t.Fatal("bounded large-radius projection failed")
	}
	if q.FindPath(context.Background(), p, p, navmesh.Filter{ExcludeFlags: 65535}).Code != navmesh.Unreachable {
		t.Fatal("excluded start allowed")
	}
}

func TestAuthoredTraversalDirectionsAndIdentity(t *testing.T) {
	scene, cfg := testworld.Scene()
	scene.Links = []geom.Link{{ID: "stairs", Revision: 1, Start: geom.Vec3{X: -3, Y: .1}, End: geom.Vec3{X: -3, Y: 3.1}, Radius: .4, Area: 2, Flags: 1}}
	w, err := dynamic.New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		s := w.Snapshot()
		found := false
		for _, l := range s.LinkStates() {
			if l.ID == "stairs" {
				found = l.Available
			}
		}
		if !found {
			t.Fatalf("unattached link: %+v", s.LinkStates())
		}
		q, _ := s.NewQuery(navmesh.Limits{})
		a, b := project(t, q, -4, .1, 0), project(t, q, -4, 3.1, 0)
		r := q.FindPath(context.Background(), a, b, navmesh.Filter{})
		if r.Code != navmesh.Complete {
			t.Fatal(r.Status)
		}
		found = false
		for _, c := range r.Path.Corners() {
			if c.Link != nil {
				if c.Link.ID != "stairs" || c.Link.Reverse {
					t.Fatalf("wrong traversal %+v", c.Link)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("missing traversal marker")
		}
		back := q.FindPath(context.Background(), b, a, navmesh.Filter{})
		if back.Code != navmesh.Partial || back.Reason != navmesh.Unreachable {
			t.Fatalf("one-way reverse %+v", back.Status)
		}
	}
	check()
	_, err = w.Apply(dynamic.Changes{Links: []geom.Link{{ID: "aaa-disabled", Start: geom.Vec3{X: 1, Y: .1}, End: geom.Vec3{X: 1, Y: 3.1}, Radius: .4, Area: 2, Flags: 1, Disabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Commit(c); err != nil {
		t.Fatal(err)
	}
	check()
}
func TestCorridorRepairAndNormals(t *testing.T) {
	_, q := setup(t)
	a, b := project(t, q, -5, 3.1, 0), project(t, q, 5, 3.1, 0)
	p := q.FindPath(context.Background(), a, b, navmesh.Filter{})
	if p.Code != navmesh.Complete {
		t.Fatal(p.Status)
	}
	m := q.Move(context.Background(), a, geom.Vec3{X: -4, Y: 3.1}, navmesh.Filter{})
	if m.Code != navmesh.Complete || math.Abs(m.Normal.Y-1) > 1e-5 {
		t.Fatalf("normal %+v", m)
	}
	repaired := q.RepairPath(context.Background(), p.Path, m.Surface, navmesh.Filter{})
	if repaired.Code != navmesh.Complete {
		t.Fatal(repaired.Status)
	}
	corners := repaired.Path.Corners()
	if math.Abs(corners[0].Position.X+4) > .01 {
		t.Fatalf("corridor not trimmed %+v", corners)
	}
	corners[0].Position.X = 99
	if repaired.Path.Corners()[0].Position.X == 99 {
		t.Fatal("route aliases caller")
	}
}

func TestTruncatedMovementKeepsStartPrefix(t *testing.T) {
	s, _ := setup(t)
	q, err := s.NewQuery(navmesh.Limits{PathPolygons: 1})
	if err != nil {
		t.Fatal(err)
	}
	a := project(t, q, -5, 3.1, 0)
	r := q.Move(context.Background(), a, geom.Vec3{X: 5, Y: 3.1}, navmesh.Filter{})
	if r.Code != navmesh.Partial || r.Reason != navmesh.ResourceExhausted || r.Surface.Position().X > 0 {
		t.Fatalf("bad movement prefix %+v", r)
	}
	if q.Height(context.Background(), r.Surface).Code != navmesh.Complete {
		t.Fatal("prefix is not on returned surface")
	}
}

func TestTruncatedRaycastSamplesActualFinalFloor(t *testing.T) {
	scene, cfg := testworld.Scene()
	scene.Instances = scene.Instances[:1]
	for i := range scene.Assets[0].Vertices {
		scene.Assets[0].Vertices[i].Y = (scene.Assets[0].Vertices[i].X + 8) * .15
	}
	r, e := bake.Build(context.Background(), scene, cfg)
	if e != nil {
		t.Fatal(e)
	}
	s, e := navmesh.NewSnapshot(r.Bundle)
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.NewQuery(navmesh.Limits{PathPolygons: 1})
	if e != nil {
		t.Fatal(e)
	}
	a := project(t, q, -5, .55, 0)
	b := project(t, q, 5, 2.05, 0)
	ray := q.Raycast(context.Background(), a, geom.Vec3{X: 5, Y: 99}, navmesh.Filter{})
	if ray.Code != navmesh.Partial || ray.Reason != navmesh.ResourceExhausted || ray.Hit || math.Abs(ray.Position.Y-b.Position().Y) > .01 {
		t.Fatalf("ray endpoint on wrong slope: %+v expectedY=%g", ray, b.Position().Y)
	}
}
func TestStreamingResults(t *testing.T) {
	scene, c := testworld.Scene()
	w, e := dynamic.New(context.Background(), scene, c)
	if e != nil {
		t.Fatal(e)
	}
	keys, e := bake.TileKeys(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.SetLoaded(keys, false); e != nil {
		t.Fatal(e)
	}
	candidate, e := w.Prepare(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Commit(candidate); e != nil {
		t.Fatal(e)
	}
	q, _ := w.Snapshot().NewQuery(navmesh.Limits{})
	r := q.Project(context.Background(), geom.Vec3{Y: .1}, navmesh.ProjectionOptions{Horizontal: .5, Up: 1, Down: 1}, navmesh.Filter{})
	if r.Code != navmesh.Unloaded {
		t.Fatalf("unloaded=%+v", r)
	}
	if _, e = w.SetLoaded(keys, true); e != nil {
		t.Fatal(e)
	}
	candidate, e = w.Prepare(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Commit(candidate); e != nil {
		t.Fatal(e)
	}
	q, _ = w.Snapshot().NewQuery(navmesh.Limits{})
	project(t, q, 0, .1, 0)
}
func BenchmarkProjection(b *testing.B) {
	_, q := setup(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Project(ctx, geom.Vec3{Y: .1}, navmesh.ProjectionOptions{Horizontal: .5, Up: .4, Down: .4}, navmesh.Filter{})
	}
}
func BenchmarkPath(b *testing.B) {
	_, q := setup(b)
	a, z := project(b, q, -5, 3.1, 0), project(b, q, 5, 3.1, 0)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.FindPath(ctx, a, z, navmesh.Filter{})
	}
}
func BenchmarkSurfaceMove(b *testing.B) {
	_, q := setup(b)
	a := project(b, q, -5, 3.1, 0)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Move(ctx, a, geom.Vec3{X: -4.9, Y: 3.1}, navmesh.Filter{})
	}
}
