package recast

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

func bridgedHolesContour(t *testing.T) (merged, outline []int, holes [][]int) {
	t.Helper()
	data, err := os.ReadFile("testdata/bridged_holes_contour.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Merged, Outline []int
		Holes           [][]int
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Merged, fixture.Outline, fixture.Holes
}

func signedContourArea(points []int) int {
	area := 0
	for i := 0; i < len(points)/4; i++ {
		j := (i + 1) % (len(points) / 4)
		area += points[i*4]*points[j*4+2] - points[j*4]*points[i*4+2]
	}
	return area
}

func TestTriangulationPreservesBridgedHoles(t *testing.T) {
	merged, outline, holes := bridgedHolesContour(t)
	original := append([]int(nil), merged...)
	indices := make([]int, len(merged)/4)
	for i := range indices {
		indices[i] = i
	}
	tris := make([]int, (len(indices)-2)*3)
	count := triangulate(len(indices), merged, indices, tris)
	if count != len(indices)-2 {
		t.Fatalf("incomplete triangulation: %d", count)
	}
	triangle := func(i int) (a, b, c []int) {
		return merged[tris[i*3]*4:], merged[tris[i*3+1]*4:], merged[tris[i*3+2]*4:]
	}
	inside := func(p, a, b, c []int) bool {
		return area2Int(a, b, p) < 0 && area2Int(b, c, p) < 0 && area2Int(c, a, p) < 0
	}
	sum := 0
	for i := 0; i < count; i++ {
		a, b, c := triangle(i)
		area := area2Int(a, b, c)
		if area >= 0 {
			t.Fatalf("triangle%d reverses or collapses: area%d", i, area)
		}
		sum += area
		for j := 0; j < i; j++ {
			d, e, f := triangle(j)
			one, two := [][]int{a, b, c}, [][]int{d, e, f}
			for k := 0; k < 3; k++ {
				for l := 0; l < 3; l++ {
					if intersectPropInt(one[k], one[(k+1)%3], two[l], two[(l+1)%3]) {
						t.Fatalf("triangles%d/%d cross", i, j)
					}
				}
				if inside(one[k], d, e, f) || inside(two[k], a, b, c) {
					t.Fatalf("triangles%d/%d overlap", i, j)
				}
			}
		}
		for h, hole := range holes {
			centerOfTriangle := []int{a[0] + b[0] + c[0], 0, a[2] + b[2] + c[2]}
			insideHole := true
			for k := 0; k < len(hole)/4; k++ {
				start, end := hole[k*4:], hole[((k+1)%(len(hole)/4))*4:]
				if area2Int([]int{start[0] * 3, 0, start[2] * 3}, []int{end[0] * 3, 0, end[2] * 3}, centerOfTriangle) <= 0 {
					insideHole = false
				}
				for _, edge := range [][2][]int{{a, b}, {b, c}, {c, a}} {
					if intersectPropInt(edge[0], edge[1], start, end) {
						t.Fatalf("triangle%d crosses hole%d boundary", i, h)
					}
				}
			}
			if insideHole {
				t.Fatalf("triangle%d lies inside hole%d", i, h)
			}
			// Convex obstacle interiors must remain excluded. Scale by vertex count so
			// centroid classification is exact integer arithmetic, without rounding.
			n := len(hole) / 4
			center := []int{0, 0, 0}
			for k := 0; k < n; k++ {
				center[0] += hole[k*4]
				center[2] += hole[k*4+2]
			}
			scale := func(p []int) []int { return []int{p[0] * n, 0, p[2] * n} }
			if inside(center, scale(a), scale(b), scale(c)) {
				t.Fatalf("triangle%d filled hole%d", i, h)
			}
		}
	}
	want := signedContourArea(outline)
	for _, hole := range holes {
		want += signedContourArea(hole)
	}
	if sum != want || sum != signedContourArea(merged) {
		t.Fatalf("changed covered area: %d want%d", sum, want)
	}
	if !reflect.DeepEqual(merged, original) {
		t.Fatal("triangulation mutated contour geometry")
	}
	set := ContourSet{Conts: []Contour{{Verts: merged, Nverts: len(merged) / 4, Reg: 1, Area: 1}}, Nconts: 1, Cs: .1, Ch: .1, Bmax: [3]float32{128, 1000, 128}}
	var mesh PolyMesh
	if err := BuildPolyMeshChecked(&set, 6, &mesh); err != nil {
		t.Fatal(err)
	}
	p := detour.DtNavMeshCreateParams{Verts: mesh.Verts, VertCount: int32(mesh.Nverts), Polys: mesh.Polys, PolyFlags: make([]uint16, mesh.Npolys), PolyAreas: mesh.Areas, PolyCount: int32(mesh.Npolys), Nvp: int32(mesh.Nvp), Cs: mesh.Cs, Ch: mesh.Ch, Bmin: mesh.Bmin, Bmax: mesh.Bmax}
	if err := detour.ValidateCreateParams(&p); err != nil {
		t.Fatalf("generated polygon topology: %v", err)
	}
}
