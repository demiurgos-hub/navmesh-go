package geom

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
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
	if r.ResolvedFingerprint != referenceResolvedFingerprint(t, r) {
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
		want := sha256.Sum256([]byte("navmesh-go resolved fingerprint v2\x00\x00" + `{"Volumes":null,"Links":null}`))
		if r.ResolvedFingerprint != hex.EncodeToString(want[:]) || r.Triangles != nil {
			t.Fatal("empty or degenerate geometry changed canonical representation")
		}
	}
}

// referenceResolvedFingerprint restates the documented resolved representation.
func referenceResolvedFingerprint(t *testing.T, r Resolved) string {
	t.Helper()
	top := []byte("navmesh-go resolved fingerprint v2\x00")
	for start := 0; start < len(r.Triangles); {
		end := start
		object := sha256.New()
		for ; end < len(r.Triangles) && r.Triangles[end].ObjectID == r.Triangles[start].ObjectID; end++ {
			tr := r.Triangles[end]
			var record [81]byte
			for i, p := range tr.Vertices {
				binary.LittleEndian.PutUint64(record[24*i:], math.Float64bits(p.X))
				binary.LittleEndian.PutUint64(record[24*i+8:], math.Float64bits(p.Y))
				binary.LittleEndian.PutUint64(record[24*i+16:], math.Float64bits(p.Z))
			}
			record[72] = tr.Area
			binary.LittleEndian.PutUint64(record[73:], uint64(tr.SourceTriangle))
			object.Write(record[:])
		}
		id := r.Triangles[start].ObjectID
		top = append(top, 1)
		top = binary.LittleEndian.AppendUint64(top, uint64(len(id)))
		top = append(top, id...)
		top = binary.LittleEndian.AppendUint64(top, uint64(end-start))
		top = object.Sum(top)
		start = end
	}
	annotations, err := json.Marshal(struct {
		Volumes []Volume
		Links   []Link
	}{r.Volumes, r.Links})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append(append(top, 0), annotations...))
	return hex.EncodeToString(sum[:])
}

func TestResolvedFingerprintSeparatesObjectsAndTriangleIdentity(t *testing.T) {
	plane := Asset{ID: "a", Vertices: []Vec3{{}, {Z: 2}, {X: 2}, {X: 2, Z: 2}}, Triangles: []Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}, {Indices: [3]uint32{2, 1, 3}, Area: 1}}}
	base := Scene{Assets: []Asset{plane}, Instances: []Instance{{ID: "left", AssetID: "a", Transform: Identity()}, {ID: "right", AssetID: "a", Transform: Translation(Vec3{X: 4})}}}
	fingerprint := func(s Scene) string {
		r, err := s.Resolve(Vec3{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if want := referenceResolvedFingerprint(t, r); r.ResolvedFingerprint != want {
			t.Fatal("resolved fingerprint differs from its documented representation")
		}
		prepared, err := s.Prepare(Vec3{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Metadata().ResolvedFingerprint != r.ResolvedFingerprint {
			t.Fatal("prepared and resolved fingerprints differ")
		}
		return r.ResolvedFingerprint
	}
	want := fingerprint(base)
	renamed := base.Clone()
	renamed.Instances[1].ID = "rightmost"
	area := base.Clone()
	area.Assets[0].Triangles[1].Area = 2
	moved := base.Clone()
	moved.Instances[1].Transform[3] = 4.5
	for name, s := range map[string]Scene{"object ID": renamed, "area": area, "vertex": moved} {
		if fingerprint(s) == want {
			t.Fatalf("resolved fingerprint ignores a changed %s", name)
		}
	}
}

func TestPreparedObjectsSummarizeResolvedPlacements(t *testing.T) {
	plane := Asset{ID: "a", Vertices: []Vec3{{}, {Z: 2}, {X: 2}}, Triangles: []Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}}}
	s := Scene{Assets: []Asset{plane}, Instances: []Instance{{ID: "b", AssetID: "a", Transform: Translation(Vec3{X: 4})}, {ID: "a", AssetID: "a", Transform: Identity()}, {ID: "off", AssetID: "a", Transform: Identity(), Disabled: true}}}
	prepared, err := s.Prepare(Vec3{}, false)
	if err != nil {
		t.Fatal(err)
	}
	objects := prepared.Objects()
	if len(objects) != 2 || objects[0].ID != "a" || objects[1].ID != "b" || objects[0].Triangles != 1 {
		t.Fatalf("objects = %+v", objects)
	}
	if objects[1].Bounds != (Bounds{Vec3{X: 4}, Vec3{X: 6, Z: 2}}) {
		t.Fatalf("bounds = %+v", objects[1].Bounds)
	}
	if objects[0].Signature == objects[1].Signature || objects[0].Signature == ([32]byte{}) {
		t.Fatal("object signatures do not identify transformed triangles")
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
