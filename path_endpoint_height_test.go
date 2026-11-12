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
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func endpointHeightFixture(t *testing.T, origin Vec3) (*Snapshot, *Query, Surface, Surface) {
	t.Helper()
	// Nine polygons from the captured multilevel ramp tile (-3,5,0), with
	// unrelated tiles and heightfield-cache arrays removed. The selected edge
	// interpolates a different coarse height from its detail surface.
	data, err := os.ReadFile("testdata/path-ramp-boundary.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle artifact.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	add := func(p Vec3) Vec3 { return Vec3{X: p.X + origin.X, Y: p.Y + origin.Y, Z: p.Z + origin.Z} }
	bundle.Settings.Origin = origin
	bundle.Settings.Bounds.Min, bundle.Settings.Bounds.Max = add(bundle.Settings.Bounds.Min), add(bundle.Settings.Bounds.Max)
	snapshot, err := NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	q, err := snapshot.NewQuery(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	project := func(p Vec3) Surface {
		t.Helper()
		r := q.Project(context.Background(), add(p), ProjectionOptions{Horizontal: .1, Up: .4, Down: .4}, Filter{})
		if r.Code != Complete {
			t.Fatal(r.Status)
		}
		return r.Surface
	}
	a := project(Vec3{X: -40.49014663696289, Y: 8.196059226989746, Z: 84.1496353149414})
	b := project(Vec3{X: -40.75, Y: 7.800000190734863, Z: 83.5})
	return snapshot, q, a, b
}

func TestGroundEndpointHeightsPreserveSelectedSurface(t *testing.T) {
	ctx := context.Background()
	for _, origin := range []Vec3{{}, {X: -500, Y: -10, Z: -900}, {X: 1e8, Y: 1e6, Z: -1e8}} {
		t.Run(stringOrigin(origin), func(t *testing.T) {
			_, q, boundary, other := endpointHeightFixture(t, origin)
			local := q.local(boundary.position)
			var coarse [3]float32
			if st := q.query.ClosestPointOnPolyBoundary(boundary.ref, local[:], coarse[:]); detour.DtStatusFailed(st) {
				t.Fatal(st)
			}
			if coarse[0] != local[0] || coarse[2] != local[2] || math.Abs(float64(coarse[1]-local[1])) < .2 {
				t.Fatalf("fixture no longer exercises the unchanged upstream boundary clamp: local=%v coarse=%v", local, coarse)
			}
			for _, endpoints := range [][2]Surface{{boundary, other}, {other, boundary}} {
				start, end := endpoints[0], endpoints[1]
				path := q.FindPath(ctx, start, end, Filter{})
				if path.Code != Complete || path.Path == nil {
					t.Fatal(path.Status)
				}
				assertEndpoints := func(p *Path) {
					t.Helper()
					corners := p.Corners()
					if len(corners) < 2 || corners[0].Position != start.Position() || corners[len(corners)-1].Position != end.Position() || p.start != start || p.End() != end {
						t.Fatalf("selected endpoint identity/height changed: corners=%+v start=%+v end=%+v", corners, start.Position(), end.Position())
					}
				}
				assertEndpoints(path.Path)
				owned := path.Path.Corners()
				repaired := q.RepairPath(ctx, path.Path, start, Filter{})
				if repaired.Code != Complete || repaired.Path == nil {
					t.Fatal(repaired.Status)
				}
				assertEndpoints(repaired.Path)
				if moved := q.MoveWithoutVisited(ctx, start, repaired.Path.corners[0].Position, Filter{}); moved.Code != Complete || moved.Surface.position != start.position {
					t.Fatalf("first corner is not current support: %+v", moved)
				}
				q.FindPath(ctx, end, start, Filter{})
				copyOut := repaired.Path.Corners()
				copyOut[0].Position.Y += 10
				if !reflect.DeepEqual(path.Path.Corners(), owned) || !reflect.DeepEqual(repaired.Path.Corners(), owned) {
					t.Fatal("endpoint correction borrowed mutable query or public result storage")
				}
			}
		})
	}
}

func stringOrigin(origin Vec3) string {
	if origin == (Vec3{}) {
		return "captured-negative-local-coordinates"
	}
	if origin.X < 0 {
		return "negative-origin"
	}
	return "large-origin"
}

func TestGroundEndpointHeightsKeepPartialBoundary(t *testing.T) {
	snapshot, start, goal := scratchChainSnapshot(t, 4, false, true)
	q, _ := snapshot.NewQuery(Limits{})
	path := q.FindPath(context.Background(), start, goal, Filter{})
	if path.Code != Partial || path.Reason != Unreachable || path.Path == nil {
		t.Fatal(path.Status)
	}
	last := path.Path.corners[len(path.Path.corners)-1].Position
	if last != path.Path.End().Position() || last == goal.Position() || path.Path.End().ref == goal.ref {
		t.Fatalf("partial endpoint was replaced with unreachable goal: %+v", path.Path.Corners())
	}
}

func TestGroundEndpointHeightsPreserveSplitTraversalAnchors(t *testing.T) {
	ctx := context.Background()
	scene, cfg := testworld.Scene()
	scene.Assets = append(scene.Assets, testworld.Plane("top", Vec3{X: -6, Y: 6, Z: -2}, Vec3{X: 6, Y: 6, Z: 2}))
	scene.Instances = append(scene.Instances, geom.Instance{ID: "top", AssetID: "top", Revision: 1, Transform: geom.Identity()})
	scene.Links = []geom.Link{
		{ID: "lower", Revision: 1, Start: Vec3{X: -3, Y: .1}, End: Vec3{X: -3, Y: 3.1}, Radius: .4, Area: 2, Flags: 1, Bidirectional: true},
		{ID: "upper", Revision: 1, Start: Vec3{X: -3, Y: 3.1}, End: Vec3{X: -3, Y: 6.1}, Radius: .4, Area: 2, Flags: 1, Bidirectional: true},
	}
	built, err := bake.Build(ctx, scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshot(built.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := snapshot.NewQuery(Limits{})
	project := func(y float64) Surface {
		t.Helper()
		r := q.Project(ctx, Vec3{X: -3, Y: y}, ProjectionOptions{Horizontal: .1, Up: .2, Down: .2}, Filter{})
		if r.Code != Complete {
			t.Fatal(r.Status)
		}
		return r.Surface
	}
	a, z := project(.1), project(6.1)
	for _, endpoints := range [][2]Surface{{a, z}, {z, a}} {
		path := q.FindPath(ctx, endpoints[0], endpoints[1], Filter{})
		if path.Code != Complete || path.Path == nil {
			t.Fatal(path.Status)
		}
		owned := path.Path.Corners()
		links := 0
		for _, c := range owned {
			if c.Link != nil {
				links++
				if c.Position != c.Link.From || c.Link.From.X != c.Link.To.X || c.Link.From.Z != c.Link.To.Z || c.Link.From.Y == c.Link.To.Y {
					t.Fatalf("authored vertical traversal changed: %+v", c)
				}
			}
		}
		if links != 2 {
			t.Fatalf("expected both mandatory traversal markers: %+v", owned)
		}
		limited, _ := snapshot.NewQuery(Limits{Corners: 2})
		truncated := limited.FindPath(ctx, endpoints[0], endpoints[1], Filter{})
		if truncated.Code != Partial || truncated.Reason != ResourceExhausted || truncated.Path == nil {
			t.Fatal(truncated.Status)
		}
		end := truncated.Path.End()
		var tile *detour.DtMeshTile
		var poly *detour.DtPoly
		if detour.DtStatusFailed(snapshot.mesh.GetTileAndPolyByRef(end.ref, &tile, &poly)) || poly.GetType() != detour.DT_POLYTYPE_GROUND || end.position.Y == endpoints[1].position.Y || math.Abs(end.position.Y-3.1) > .001 {
			t.Fatalf("truncated landing was rewritten to requested destination: %+v", end)
		}
		if !reflect.DeepEqual(path.Path.Corners(), owned) {
			t.Fatal("truncated query mutated the owned complete path")
		}
	}
}
