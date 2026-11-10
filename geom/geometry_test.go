package geom

import (
	"math"
	"reflect"
	"testing"
)

func source() Scene {
	return Scene{Assets: []Asset{{ID: "floor", Vertices: []Vec3{{}, {Z: 2}, {X: 2, Z: 2}}, Triangles: []Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}}}}, Instances: []Instance{{ID: "floor-1", AssetID: "floor", Transform: Identity()}}}
}
func TestReflectedTransformedInstance(t *testing.T) {
	s := source()
	s.Instances[0].Transform = Transform{-2, 0, 0, 1000000, 0, 1, 0, 4, 0, 0, 3, -1000000, 0, 0, 0, 1}
	r, e := s.Resolve(Vec3{X: 1000000, Z: -1000000}, true)
	if e != nil {
		t.Fatal(e)
	}
	v := r.Triangles[0].Vertices
	a, b := sub(v[1], v[0]), sub(v[2], v[0])
	if a.Z*b.X-a.X*b.Z <= 0 {
		t.Fatal("reflection reversed floor")
	}
	if r.Triangles[0].Bounds.Min.X != -4 || r.Triangles[0].Bounds.Max.Z != 6 || v[0].Y != 4 {
		t.Fatalf("bad transform %+v", v)
	}
	r.Triangles[0].Vertices[0].X = 99
	if s.Assets[0].Vertices[0].X != 0 {
		t.Fatal("input aliased")
	}
}

func TestSourceFingerprintIndependentOfResolutionOrigin(t *testing.T) {
	s := source()
	s.Volumes = []Volume{{ID: "area", Shape: "convex", Area: 2, Bounds: Bounds{Min: Vec3{X: 100, Z: 100}, Max: Vec3{X: 102, Y: 2, Z: 102}}, Points: []Vec3{{X: 100, Z: 100}, {X: 100, Z: 102}, {X: 102, Z: 102}, {X: 102, Z: 100}}}}
	want, e := s.Fingerprint()
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.Resolve(Vec3{}, false)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Resolve(Vec3{X: 100, Z: 100}, false)
	if e != nil {
		t.Fatal(e)
	}
	if a.Fingerprint != want || b.Fingerprint != want {
		t.Fatal("origin changed source identity")
	}
	if a.ResolvedFingerprint == b.ResolvedFingerprint {
		t.Fatal("resolved identity ignored origin")
	}
	if s.Volumes[0].Points[0].X != 100 {
		t.Fatal("resolution changed source")
	}
}
func TestGeometryValidation(t *testing.T) {
	s := source()
	s.Instances[0].Transform = Transform{}
	if _, e := s.Resolve(Vec3{}, false); e == nil {
		t.Fatal("singular accepted")
	}
	s = source()
	s.Assets[0].Vertices[0].X = math.NaN()
	if _, e := s.Resolve(Vec3{}, false); e == nil {
		t.Fatal("nonfinite accepted")
	}
	s = source()
	s.Assets[0].Triangles[0].Indices = [3]uint32{0, 0, 0}
	r, e := s.Resolve(Vec3{}, false)
	if e != nil || len(r.Triangles) != 0 || len(r.Diagnostics) != 1 {
		t.Fatal("degenerate diagnostics", e)
	}
	if _, e = s.Resolve(Vec3{}, true); e == nil {
		t.Fatal("strict accepted degenerate")
	}
}
func TestConvexBoundsAndIdentity(t *testing.T) {
	s := source()
	s.Volumes = []Volume{{ID: "hole", Shape: "convex", Exclude: true, Bounds: Bounds{Min: Vec3{}, Max: Vec3{X: 2, Y: 2, Z: 2}}, Points: []Vec3{{}, {X: 2}, {X: 1, Z: .5}, {X: 2, Z: 2}, {Z: 2}}}}
	if _, e := s.Resolve(Vec3{}, false); e == nil {
		t.Fatal("concave polygon accepted")
	}
	s.Volumes[0].Points = []Vec3{{}, {X: 3}, {Z: 2}}
	if _, e := s.Resolve(Vec3{}, false); e == nil {
		t.Fatal("outside volume bounds accepted")
	}
	if LinkKey("door") != LinkKey("door") || LinkKey("door") == LinkKey("bridge") {
		t.Fatal("bad stable IDs")
	}
}
func TestIndexPreservesSourceOrder(t *testing.T) {
	var tris []ResolvedTriangle
	for i := 0; i < 100; i++ {
		x := float64((i * 17) % 100)
		tris = append(tris, ResolvedTriangle{Bounds: Bounds{Min: Vec3{X: x}, Max: Vec3{X: x + 1, Y: 1, Z: 1}}})
	}
	box := Bounds{Min: Vec3{X: 25}, Max: Vec3{X: 50, Y: 1, Z: 1}}
	var expected []int
	for i, tr := range tris {
		if tr.Bounds.Intersects(box) {
			expected = append(expected, i)
		}
	}
	if got := NewIndex(tris).Query(box); !reflect.DeepEqual(got, expected) {
		t.Fatalf("%v != %v", got, expected)
	}
}
