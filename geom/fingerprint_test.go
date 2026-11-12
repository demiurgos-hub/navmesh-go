package geom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestFingerprintPreservesCanonicalMarshalBytes(t *testing.T) {
	s := Scene{Assets: []Asset{{ID: "z", Vertices: []Vec3{}, Triangles: []Triangle{}}, {ID: "a", Vertices: []Vec3{{X: -0.0, Y: 2, Z: 3}}}}, Volumes: []Volume{{ID: "volume", Points: []Vec3{}}}, Links: []Link{{ID: "link<>&\n", Profiles: []string{"z", "a"}, Action: "\u2028\n"}}}
	before := s.Clone()
	canonical := s.Clone()
	sort.Slice(canonical.Assets, func(i, j int) bool { return canonical.Assets[i].ID < canonical.Assets[j].ID })
	sort.Strings(canonical.Links[0].Profiles)
	data, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	got, err := s.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(want[:]) {
		t.Fatal("persisted fingerprint representation changed")
	}
	if !reflect.DeepEqual(before, s.Clone()) {
		t.Fatal("fingerprinting reordered caller slices")
	}
}

func TestResolveDoesNotBorrowMutableSourceArrays(t *testing.T) {
	s := Scene{Assets: []Asset{{ID: "a", Vertices: []Vec3{{}, {Z: 2}, {X: 2}}, Triangles: []Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}}}}, Instances: []Instance{{ID: "floor", AssetID: "a", Transform: Identity()}}, Volumes: []Volume{{ID: "area", Shape: "convex", Bounds: Bounds{Min: Vec3{}, Max: Vec3{X: 2, Y: 2, Z: 2}}, Points: []Vec3{{}, {Z: 2}, {X: 2}}, Area: 1}}, Links: []Link{{ID: "link", Start: Vec3{}, End: Vec3{X: 1}, Radius: .2, Flags: 1, Profiles: []string{"z", "a"}}}}
	r, err := s.Resolve(Vec3{}, false)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(r)
	canonical, err := json.Marshal(struct {
		Triangles []ResolvedTriangle
		Volumes   []Volume
		Links     []Link
	}{r.Triangles, r.Volumes, r.Links})
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(canonical)
	if r.ResolvedFingerprint != hex.EncodeToString(want[:]) {
		t.Fatal("resolved fingerprint wire representation changed")
	}
	s.Assets[0].Vertices[0].X = 99
	s.Assets[0].Triangles[0].Area = 63
	s.Volumes[0].Points[0].X = 99
	s.Links[0].Profiles[0] = "changed"
	after, _ := json.Marshal(r)
	if string(before) != string(after) {
		t.Fatal("resolved data aliases caller arrays")
	}
}

func TestEmptyResolvedFingerprintPreservesNullArrays(t *testing.T) {
	for _, s := range []Scene{{}, {Assets: []Asset{{ID: "point", Vertices: []Vec3{{}}, Triangles: []Triangle{{}}}}, Instances: []Instance{{ID: "point", AssetID: "point", Transform: Identity()}}}} {
		r, err := s.Resolve(Vec3{}, false)
		if err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256([]byte(`{"Triangles":null,"Volumes":null,"Links":null}`))
		if r.ResolvedFingerprint != hex.EncodeToString(want[:]) || r.Triangles != nil {
			t.Fatal("empty or degenerate geometry changed canonical representation")
		}
	}
}

func TestResolveStreamsLargeArrays(t *testing.T) {
	makeScene := func(count int) Scene {
		triangles := make([]Triangle, count)
		for i := range triangles {
			triangles[i] = Triangle{Indices: [3]uint32{0, 1, 2}, Area: 1}
		}
		return Scene{Assets: []Asset{{ID: "mesh", Vertices: []Vec3{{}, {Z: 2}, {X: 2}}, Triangles: triangles}}, Instances: []Instance{{ID: "mesh", AssetID: "mesh", Transform: Identity()}}}
	}
	small, large := makeScene(8), makeScene(2048)
	r, err := large.Resolve(Vec3{}, false)
	if err != nil || len(r.Triangles) != 2048 {
		t.Fatal("large source resolution failed", err)
	}
	t.Run("ResolutionAllocations", func(t *testing.T) {
		measure := func(scene Scene) float64 {
			return testing.AllocsPerRun(5, func() {
				if _, err := scene.Resolve(Vec3{}, false); err != nil {
					panic(err)
				}
			})
		}
		few, many := measure(small), measure(large)
		if many > few+4 {
			t.Fatalf("allocation count scales with triangle count: small=%v large=%v", few, many)
		}
	})
}
