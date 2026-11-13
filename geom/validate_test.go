package geom

import (
	"math"
	"reflect"
	"runtime"
	"testing"
)

func TestSceneValidatePreservesResolveAdmission(t *testing.T) {
	tests := []struct {
		name               string
		change             func(*Scene, *Vec3)
		valid, strictValid bool
	}{
		{"valid", func(*Scene, *Vec3) {}, true, true},
		{"reflection", func(s *Scene, _ *Vec3) { s.Instances[0].Transform[0] = -2 }, true, true},
		{"degenerate", func(s *Scene, _ *Vec3) { s.Assets[0].Triangles[0].Indices = [3]uint32{} }, true, false},
		{"disabled degenerate", func(s *Scene, _ *Vec3) {
			s.Assets[0].Triangles[0].Indices = [3]uint32{}
			s.Instances[0].Disabled = true
		}, true, true},
		{"invalid origin", func(_ *Scene, o *Vec3) { o.X = math.Inf(1) }, false, false},
		{"duplicate asset", func(s *Scene, _ *Vec3) { s.Assets = append(s.Assets, s.Assets[0]) }, false, false},
		{"duplicate instance", func(s *Scene, _ *Vec3) { s.Instances = append(s.Instances, s.Instances[0]) }, false, false},
		{"missing asset", func(s *Scene, _ *Vec3) { s.Instances[0].AssetID = "missing" }, false, false},
		{"invalid vertex", func(s *Scene, _ *Vec3) { s.Assets[0].Vertices[0].Y = math.NaN() }, false, false},
		{"invalid index", func(s *Scene, _ *Vec3) { s.Assets[0].Triangles[0].Indices[1] = 99 }, false, false},
		{"invalid area", func(s *Scene, _ *Vec3) { s.Assets[0].Triangles[0].Area = 64 }, false, false},
		{"singular disabled transform", func(s *Scene, _ *Vec3) { s.Instances[0].Transform = Transform{}; s.Instances[0].Disabled = true }, false, false},
		{"transform overflow", func(s *Scene, _ *Vec3) {
			s.Instances[0].Transform[3] = math.MaxFloat64
			s.Assets[0].Vertices[0].X = math.MaxFloat64
		}, false, false},
		{"normal overflow", func(s *Scene, _ *Vec3) { s.Assets[0].Vertices = []Vec3{{}, {Z: 1e200}, {X: 1e200}} }, false, false},
		{"noncylinder NaN radius", func(s *Scene, _ *Vec3) { s.Volumes[0].Radius = math.NaN() }, false, false},
		{"disabled noncylinder infinite radius", func(s *Scene, _ *Vec3) { s.Volumes[0].Radius = math.Inf(1); s.Volumes[0].Disabled = true }, false, false},
		{"volume bounds overflow", func(s *Scene, o *Vec3) {
			s.Instances = nil
			s.Volumes[0].Bounds.Min.X = math.MaxFloat64
			s.Volumes[0].Bounds.Max.X = math.MaxFloat64
			o.X = -math.MaxFloat64
		}, false, false},
		{"volume point overflow", func(s *Scene, o *Vec3) {
			s.Instances = nil
			s.Volumes[0].Points = []Vec3{{X: math.MaxFloat64}}
			o.X = -math.MaxFloat64
		}, false, false},
		{"link overflow", func(s *Scene, o *Vec3) {
			s.Instances = nil
			s.Links[0].Start.X = math.MaxFloat64
			o.X = -math.MaxFloat64
		}, false, false},
		{"disabled transform overflow", func(s *Scene, o *Vec3) {
			s.Instances = nil
			s.Volumes[0].Disabled = true
			s.Volumes[0].Bounds.Min.X = math.MaxFloat64
			s.Volumes[0].Bounds.Max.X = math.MaxFloat64
			s.Volumes[0].Points = []Vec3{{X: math.MaxFloat64}}
			s.Links[0].Disabled = true
			s.Links[0].Start.X = math.MaxFloat64
			o.X = -math.MaxFloat64
		}, true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, strict := range []bool{false, true} {
				s := source()
				s.Volumes = []Volume{{ID: "paint", Shape: "box", Bounds: Bounds{Min: Vec3{}, Max: Vec3{X: 2, Y: 2, Z: 2}}, Area: 1}}
				s.Links = []Link{{ID: "door", Start: Vec3{}, End: Vec3{X: 1}, Radius: .2, Flags: 1, Profiles: []string{"b", "a"}}}
				origin := Vec3{}
				test.change(&s, &origin)
				want := test.valid
				if strict {
					want = test.strictValid
				}
				_, resolvedErr := s.Resolve(origin, strict)
				validatedErr := s.Validate(origin, strict)
				if (resolvedErr == nil) != want || (validatedErr == nil) != want {
					t.Fatalf("strict=%v expected valid=%v; Resolve=%v Validate=%v", strict, want, resolvedErr, validatedErr)
				}
			}
		})
	}
}

func TestValidationWalkStreamsExactResolvedTrianglesWithoutAliasing(t *testing.T) {
	s := source()
	s.Instances = append(s.Instances, Instance{ID: "reflected", AssetID: "floor", Transform: Transform{-2, 0, 0, 4, 0, 1, 0, 0, 0, 0, 3, 0, 0, 0, 0, 1}})
	s.Assets[0].Triangles = append(s.Assets[0].Triangles, Triangle{Indices: [3]uint32{0, 0, 0}})
	s.Volumes = []Volume{{ID: "paint", Shape: "convex", Area: 1, Bounds: Bounds{Min: Vec3{}, Max: Vec3{X: 2, Y: 2, Z: 2}}, Points: []Vec3{{}, {X: 2}, {X: 2, Z: 2}, {Z: 2}}}}
	s.Links = []Link{{ID: "door", Start: Vec3{}, End: Vec3{X: 1}, Radius: .2, Flags: 1, Profiles: []string{"b", "a"}}}
	before := s.Clone()
	origin := Vec3{X: 1, Z: -1}
	full, err := s.Resolve(origin, false)
	if err != nil {
		t.Fatal(err)
	}
	var streamed []ResolvedTriangle
	metadata, err := s.resolve(origin, false, resolveOptions{retainMetadata: true, triangle: func(triangle *ResolvedTriangle) { streamed = append(streamed, *triangle) }})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(streamed, full.Triangles) || !reflect.DeepEqual(metadata.Volumes, full.Volumes) || !reflect.DeepEqual(metadata.Links, full.Links) || !reflect.DeepEqual(metadata.Diagnostics, full.Diagnostics) {
		t.Fatal("streaming walk changed resolved geometry or metadata")
	}
	if metadata.Triangles != nil || metadata.Fingerprint != "" || metadata.ResolvedFingerprint != "" {
		t.Fatal("streaming validation retained triangles or computed unused hashes")
	}
	if err = s.Validate(origin, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("validation changed authored source")
	}
	metadata.Volumes[0].Points[0].X++
	metadata.Links[0].Profiles[0] = "changed"
	if !reflect.DeepEqual(s, before) {
		t.Fatal("retained metadata aliases authored source")
	}
}

func TestSceneValidateDoesNotAllocateExpandedTriangleStorage(t *testing.T) {
	s := source()
	s.Assets[0].Triangles = make([]Triangle, 100000)
	for i := range s.Assets[0].Triangles {
		s.Assets[0].Triangles[i] = Triangle{Indices: [3]uint32{0, 1, 2}, Area: 1}
	}
	if err := s.Validate(Vec3{}, true); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := s.Validate(Vec3{}, true)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	// The resolved triangle array alone exceeds 14 MiB. Validation needs only
	// identity bookkeeping for this one asset and placement, not that array.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("validation allocated %d bytes for one shared placement", allocated)
	}
}
