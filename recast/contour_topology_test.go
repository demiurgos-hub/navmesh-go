package recast

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

func crossingWallContour(t *testing.T) (points, prior []int) {
	t.Helper()
	data, err := os.ReadFile("testdata/crossing_wall_contour.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Points []int `json:"points"`
		Prior  []int `json:"prior_simplified"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Points, fixture.Prior
}

func TestSimplificationRetainsSimpleWallTopology(t *testing.T) {
	points, prior := crossingWallContour(t)
	// Captured from an unchanged authored-world tile: the distance-only nine-point
	// contour crossed its closing chord and produced a counterclockwise triangle.
	params := func(contour []int) (detour.DtNavMeshCreateParams, error) {
		t.Helper()
		set := ContourSet{Conts: []Contour{{Verts: contour, Nverts: len(contour) / 4, Reg: 1, Area: 1}}, Nconts: 1, Cs: .1, Ch: .1, Bmax: [3]float32{128, 1000, 128}}
		var mesh PolyMesh
		if err := BuildPolyMeshChecked(&set, 6, &mesh); err != nil {
			return detour.DtNavMeshCreateParams{}, err
		}
		return detour.DtNavMeshCreateParams{Verts: mesh.Verts, VertCount: int32(mesh.Nverts), Polys: mesh.Polys, PolyFlags: make([]uint16, mesh.Npolys), PolyAreas: mesh.Areas, PolyCount: int32(mesh.Npolys), Nvp: int32(mesh.Nvp), Cs: mesh.Cs, Ch: mesh.Ch, Bmin: mesh.Bmin, Bmax: mesh.Bmax}, nil
	}
	before, err := params(prior)
	if err == nil && detour.ValidateCreateParams(&before) == nil {
		t.Fatal("crossing outline was accepted as valid geometry")
	}
	after := simplifyContour(points, 2, 0, int(contourTessWallEdges))
	if len(after) <= len(prior) || len(after) > len(points) {
		t.Fatalf("expected bounded raw-point refinement: %d -> %d (raw%d)", len(prior)/4, len(after)/4, len(points)/4)
	}
	p, err := params(after)
	if err != nil {
		t.Fatal(err)
	}
	if err := detour.ValidateCreateParams(&p); err != nil {
		t.Fatalf("refined contour violates Detour topology: %v", err)
	}
	for i := 0; i < len(after); i += 4 {
		found := false
		for j := 0; j < len(points); j += 4 {
			if after[i] == points[j] && after[i+1] == points[j+1] && after[i+2] == points[j+2] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("refinement synthesized a coordinate: %v", after[i:i+3])
		}
	}
}
