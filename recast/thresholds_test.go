package recast_test

import (
	"context"
	"fmt"
	"math"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func thresholdPlane(id string, xmin, xmax, zmin, zmax, leftY, rightY float64, area uint8) geom.Asset {
	return geom.Asset{ID: id, Vertices: []geom.Vec3{{X: xmin, Y: leftY, Z: zmin}, {X: xmin, Y: leftY, Z: zmax}, {X: xmax, Y: rightY, Z: zmax}, {X: xmax, Y: rightY, Z: zmin}}, Triangles: []geom.Triangle{{Indices: [3]uint32{0, 1, 2}, Area: area}, {Indices: [3]uint32{0, 2, 3}, Area: area}}}
}

func thresholdScene(assets ...geom.Asset) geom.Scene {
	s := geom.Scene{Assets: assets}
	for _, a := range assets {
		s.Instances = append(s.Instances, geom.Instance{ID: a.ID, AssetID: a.ID, Transform: geom.Identity()})
	}
	return s
}

func thresholdConfig() bake.Config {
	c := bake.DefaultConfig("threshold-corpus", geom.Bounds{Min: geom.Vec3{X: -6, Y: -1, Z: -6}, Max: geom.Vec3{X: 6, Y: 16, Z: 6}})
	c.Settings.Origin = geom.Vec3{X: -8, Y: -1, Z: -8}
	c.Settings.CellSize, c.Settings.CellHeight = .25, .25
	c.Settings.TileSize = 64
	c.Profile.Height, c.Profile.Radius, c.Profile.MaxClimb = 1.5, .25, .5
	return c
}

func thresholdQuery(t *testing.T, s geom.Scene, c bake.Config) (*navmesh.Snapshot, *navmesh.Query) {
	t.Helper()
	r, err := bake.Build(context.Background(), s, c)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := navmesh.NewSnapshot(r.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	q, err := snapshot.NewQuery(navmesh.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, q
}

func thresholdProject(q *navmesh.Query, x, y, z float64) navmesh.ProjectionResult {
	return q.Project(context.Background(), geom.Vec3{X: x, Y: y, Z: z}, navmesh.ProjectionOptions{Horizontal: .1, Up: .3, Down: .3}, navmesh.Filter{})
}

func TestBakedSlopeThresholds(t *testing.T) {
	for _, degrees := range []float64{44.9, 45.1} {
		t.Run(fmt.Sprint(degrees), func(t *testing.T) {
			height := 12 * math.Tan(degrees*math.Pi/180)
			s := thresholdScene(thresholdPlane("slope", -6, 6, -6, 6, 0, height, 1))
			c := thresholdConfig()
			c.Profile.MaxClimb = 1 // Isolate slope classification from step quantization.
			_, q := thresholdQuery(t, s, c)
			// Coarse 0.25m vertical cells lift this steep plane more than the
			// small default projection allowance. Both Go and pinned C++ place
			// its center near Y6.28458 rather than raw-source Y5.97909.
			// Bound projection by voxel/detail error without changing slope.
			vertical := 2*c.Settings.CellHeight + c.Settings.DetailSampleMaxError
			p := q.Project(context.Background(), geom.Vec3{Y: height / 2}, navmesh.ProjectionOptions{Horizontal: .1, Up: vertical, Down: vertical}, navmesh.Filter{})
			if (p.Code == navmesh.Complete) != (degrees < 45) {
				t.Fatalf("slope %g: %+v", degrees, p.Status)
			}
			if p.Code == navmesh.Complete && math.Abs(p.Surface.Position().Y-height/2) > vertical {
				t.Fatalf("slope %g exceeded voxel/detail height allowance: %+v", degrees, p.Surface.Position())
			}
		})
	}
}

func TestBakedClimbThresholds(t *testing.T) {
	for _, height := range []float64{.25, .5, .75} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			s := thresholdScene(thresholdPlane("lower", -6, 0, -6, 6, 0, 0, 1), thresholdPlane("upper", 0, 6, -6, 6, height, height, 1))
			_, q := thresholdQuery(t, s, thresholdConfig())
			a, b := thresholdProject(q, -3, .25, 0), thresholdProject(q, 3, height+.25, 0)
			if a.Code != navmesh.Complete || b.Code != navmesh.Complete {
				t.Fatalf("step endpoints missing: %+v %+v", a.Status, b.Status)
			}
			path := q.FindPath(context.Background(), a.Surface, b.Surface, navmesh.Filter{})
			if (path.Code == navmesh.Complete) != (height <= .5) {
				t.Fatalf("step %g: %+v", height, path.Status)
			}
		})
	}
}

func TestBakedCeilingThresholds(t *testing.T) {
	for _, height := range []float64{1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			s := thresholdScene(thresholdPlane("ground", -6, 6, -6, 6, 0, 0, 1), thresholdPlane("ceiling", -6, 6, -6, 6, height, height, 0))
			_, q := thresholdQuery(t, s, thresholdConfig())
			p := thresholdProject(q, 0, .25, 0)
			// Rasterization occupies one floor cell: the first accepted source
			// gap is 1.5m agent height + 0.25m floor voxel, conservatively.
			if (p.Code == navmesh.Complete) != (height >= 1.75) {
				t.Fatalf("ceiling %g: %+v", height, p.Status)
			}
		})
	}
}

func TestBakedRadiusAndNarrowPassage(t *testing.T) {
	for _, radius := range []float64{.25, .5, .75, 1} {
		t.Run(fmt.Sprint(radius), func(t *testing.T) {
			s := thresholdScene(thresholdPlane("ground", -6, 6, -6, 6, 0, 0, 1))
			s.Volumes = []geom.Volume{
				{ID: "left", Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: -6, Y: -.5, Z: -6}, Max: geom.Vec3{X: -1, Y: 2, Z: 6}}},
				{ID: "right", Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: 1, Y: -.5, Z: -6}, Max: geom.Vec3{X: 6, Y: 2, Z: 6}}},
			}
			c := thresholdConfig()
			c.Profile.Radius = radius
			_, q := thresholdQuery(t, s, c)
			a, b := thresholdProject(q, .125, .25, -3), thresholdProject(q, .125, .25, 3)
			complete := a.Code == navmesh.Complete && b.Code == navmesh.Complete
			if complete {
				complete = q.FindPath(context.Background(), a.Surface, b.Surface, navmesh.Filter{}).Code == navmesh.Complete
			}
			// At radius .75 the remaining eroded strip is too thin to form
			// a positive-area polygon. The pinned C++ oracle confirms the
			// same conservative loss; radius .5 retains a usable corridor.
			if complete != (radius <= .5) {
				t.Fatalf("radius %g in 2m passage: route=%v endpoints=%+v %+v", radius, complete, a.Status, b.Status)
			}
		})
	}
}

func TestNegativeOriginThresholdParity(t *testing.T) {
	s := thresholdScene(thresholdPlane("lower", -6, 0, -6, 6, 0, 0, 1), thresholdPlane("upper", 0, 6, -6, 6, .5, .5, 1))
	c := thresholdConfig()
	_, initial := thresholdQuery(t, s, c)
	offset := geom.Vec3{X: -4096, Y: 128, Z: -8192}
	for i := range s.Instances {
		s.Instances[i].Transform = geom.Translation(offset)
	}
	add := func(v geom.Vec3) geom.Vec3 { return geom.Vec3{X: v.X + offset.X, Y: v.Y + offset.Y, Z: v.Z + offset.Z} }
	c.Settings.Origin = add(c.Settings.Origin)
	c.Settings.Bounds.Min, c.Settings.Bounds.Max = add(c.Settings.Bounds.Min), add(c.Settings.Bounds.Max)
	_, translated := thresholdQuery(t, s, c)
	a, b := thresholdProject(initial, -3, .25, 0), thresholdProject(initial, 3, .75, 0)
	x, y := thresholdProject(translated, -3+offset.X, .25+offset.Y, offset.Z), thresholdProject(translated, 3+offset.X, .75+offset.Y, offset.Z)
	if a.Code != x.Code || b.Code != y.Code || x.Code != navmesh.Complete || y.Code != navmesh.Complete {
		t.Fatalf("translated projection differs: %+v %+v %+v %+v", a.Status, b.Status, x.Status, y.Status)
	}
	p := initial.FindPath(context.Background(), a.Surface, b.Surface, navmesh.Filter{})
	q := translated.FindPath(context.Background(), x.Surface, y.Surface, navmesh.Filter{})
	if p.Code != navmesh.Complete || q.Code != p.Code || len(p.Path.Corners()) != len(q.Path.Corners()) {
		t.Fatalf("translated connectivity differs: %+v %+v", p.Status, q.Status)
	}
	pc, qc := p.Path.Corners(), q.Path.Corners()
	for i := range pc {
		want := add(pc[i].Position)
		if math.Abs(qc[i].Position.X-want.X) > 1e-5 || math.Abs(qc[i].Position.Y-want.Y) > 1e-5 || math.Abs(qc[i].Position.Z-want.Z) > 1e-5 {
			t.Fatalf("translated corner differs: got %+v want %+v", qc[i].Position, want)
		}
	}
}
