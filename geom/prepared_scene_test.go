package geom

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func instancedFixture() Scene {
	a := Asset{ID: "shared", Vertices: []Vec3{{}, {X: 2}, {Z: 3}, {Y: 2}, {X: 1e-100}}, Triangles: []Triangle{
		{Indices: [3]uint32{0, 2, 1}, Area: 1}, {Indices: [3]uint32{0, 1, 3}, Area: 2},
		{Indices: [3]uint32{0, 0, 0}}, {Indices: [3]uint32{0, 3, 2}, Area: 3},
		{Indices: [3]uint32{0, 4, 1}},
	}}
	s := Scene{Assets: []Asset{a}}
	for i := 0; i < 36; i++ {
		tr := Identity()
		tr[0], tr[1], tr[2] = float64(1+i%3), .17, -.03
		tr[5], tr[6], tr[8], tr[10] = .5, .12, .3, 1.4
		if i%2 == 0 {
			tr[0] = -tr[0]
		}
		tr[3], tr[7], tr[11] = float64(i%6)*4, float64(i%3), float64(i/6)*4
		s.Instances = append(s.Instances, Instance{ID: fmt.Sprintf("placement-%02d", 35-i), AssetID: a.ID, Transform: tr, Disabled: i%11 == 0})
	}
	return s
}

func TestPreparedSceneExactResolvedParity(t *testing.T) {
	for _, s := range []Scene{{}, source(), instancedFixture(), {Assets: []Asset{{ID: "empty"}}, Instances: []Instance{{ID: "empty", AssetID: "empty", Transform: Identity()}}}} {
		for _, origin := range []Vec3{{}, {X: 17, Y: -3, Z: 9}} {
			expected, err := s.Resolve(origin, false)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := s.Prepare(origin, false)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.TriangleCount() != len(expected.Triangles) {
				t.Fatal("triangle count changed")
			}
			meta := prepared.Metadata()
			triangles := expected.Triangles
			expected.Triangles = nil
			if !reflect.DeepEqual(expected, meta) {
				t.Fatalf("metadata/hash mismatch\n%+v\n%+v", expected, meta)
			}
			for i, want := range triangles {
				got, ok := prepared.TriangleAt(i)
				if !ok || got != want {
					t.Fatalf("triangle %d changed: %+v != %+v", i, got, want)
				}
			}
			if _, ok := prepared.TriangleAt(-1); ok {
				t.Fatal("negative index")
			}
			if _, ok := prepared.TriangleAt(len(triangles)); ok {
				t.Fatal("past end")
			}
			index := prepared.NewIndex()
			random := rand.New(rand.NewSource(11))
			boxes := []Bounds{{Min: Vec3{-100, -100, -100}, Max: Vec3{100, 100, 100}}}
			for i := 0; i < 80; i++ {
				x, z := random.Float64()*40-10, random.Float64()*40-10
				boxes = append(boxes, Bounds{Vec3{x, -10, z}, Vec3{x + 3, 10, z + 3}})
			}
			for _, tr := range triangles {
				boxes = append(boxes, Bounds{tr.Vertices[0], tr.Vertices[0]})
			}
			for _, box := range boxes {
				var want, got []ResolvedTriangle
				for _, tr := range triangles {
					if tr.Bounds.Intersects(box) {
						want = append(want, tr)
					}
				}
				if err := index.Visit(box, func(tr ResolvedTriangle) error { got = append(got, tr); return nil }); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("query mismatch for %+v: %d != %d", box, len(got), len(want))
				}
			}
		}
	}
}

func TestPreparedSceneStoresCanonicalGeometryOnce(t *testing.T) {
	scene := instancedFixture()
	prepared, err := scene.Prepare(Vec3{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.assets) != 1 || len(prepared.assets[0].Triangles) != 5 || len(prepared.metadata.Triangles) != 0 {
		t.Fatal("preparation expanded shared geometry")
	}
	index := prepared.NewIndex()
	if len(index.assets) != 1 || len(index.assets[0].bounds) != 5 {
		t.Fatal("index expanded shared geometry")
	}
	before, _ := prepared.TriangleAt(0)
	scene.Assets[0].Vertices[0] = Vec3{X: math.NaN()}
	scene.Assets[0].Triangles[0].Area = 63
	scene.Instances[0].Transform = Transform{}
	after, _ := prepared.TriangleAt(0)
	if before != after {
		t.Fatal("prepared source aliases caller")
	}
	metadata := prepared.Metadata()
	metadata.Diagnostics[0].Message = "modified"
	if prepared.Metadata().Diagnostics[0].Message == "modified" {
		t.Fatal("metadata aliases source")
	}
}

func TestPreparedSceneDegeneracyDependsOnPlacement(t *testing.T) {
	s := source()
	s.Instances = append(s.Instances, Instance{ID: "collapsed", AssetID: "floor", Transform: Translation(Vec3{X: 1e20, Y: 1e20, Z: 1e20})})
	want, err := s.Resolve(Vec3{}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Prepare(Vec3{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.TriangleCount() != 1 || len(got.Metadata().Diagnostics) != 1 {
		t.Fatal("placement-specific degeneracy lost")
	}
	tr, _ := got.TriangleAt(0)
	if tr != want.Triangles[0] || got.Metadata().ResolvedFingerprint != want.ResolvedFingerprint {
		t.Fatal("placement omission changed")
	}
	if _, err := s.Prepare(Vec3{}, true); err == nil {
		t.Fatal("strict degeneration accepted")
	}
}

func TestTransformedBoundsConservative(t *testing.T) {
	random := rand.New(rand.NewSource(91))
	for i := 0; i < 5000; i++ {
		magnitude := math.Pow(10, float64(i%25)-12)
		b := Bounds{Vec3{-3 * magnitude, -2 * magnitude, -magnitude}, Vec3{magnitude, 2 * magnitude, 3 * magnitude}}
		tr := Identity()
		for j := 0; j < 12; j++ {
			tr[j] = (random.Float64() - .5) * math.Pow(10, float64((i+j)%19)-9)
		}
		origin := Vec3{tr[3], -tr[7], tr[11]}
		bound := transformedBounds(b, tr, origin)
		for j := 0; j < 16; j++ {
			point := Vec3{b.Min.X + (b.Max.X-b.Min.X)*random.Float64(), b.Min.Y + (b.Max.Y-b.Min.Y)*random.Float64(), b.Min.Z + (b.Max.Z-b.Min.Z)*random.Float64()}
			if j < 8 {
				point = b.Min
				if j&1 != 0 {
					point.X = b.Max.X
				}
				if j&2 != 0 {
					point.Y = b.Max.Y
				}
				if j&4 != 0 {
					point.Z = b.Max.Z
				}
			}
			point = sub(tr.Point(point), origin)
			if !bound.Intersects(Bounds{point, point}) {
				t.Fatalf("transform bounds exclude point %+v: %+v", point, bound)
			}
		}
	}
}
