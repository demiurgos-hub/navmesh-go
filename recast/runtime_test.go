package recast

import (
	"context"
	"math"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

func TestBakedStackedSurfacesRemainDisconnectedInDetour(t *testing.T) {
	var verts []float32
	var tris []int
	addTestFloor(&verts, &tris, -6, -6, 6, 6, 0)
	addTestFloor(&verts, &tris, -2, -2, 2, 2, 3)
	c := buildTestConfig()
	for _, mode := range []Partition{Monotone, Watershed, LayerRegions} {
		r, err := Build(context.Background(), c, verts, tris, nil, BuildOptions{Partition: mode})
		if err != nil {
			t.Fatal(err)
		}
		m, d := r.Mesh, r.Detail
		for i := range m.Flags {
			m.Flags[i] = 1
		}
		p := detour.DtNavMeshCreateParams{Verts: m.Verts, VertCount: int32(m.Nverts), Polys: m.Polys, PolyFlags: m.Flags, PolyAreas: m.Areas, PolyCount: int32(m.Npolys), Nvp: int32(m.Nvp), DetailMeshes: d.Meshes, DetailVerts: d.Verts, DetailVertsCount: int32(d.Nverts), DetailTris: d.Tris, DetailTriCount: int32(d.Ntris), Bmin: m.Bmin, Bmax: m.Bmax, WalkableHeight: 1.8, WalkableRadius: .5, WalkableClimb: .3, Cs: c.Cs, Ch: c.Ch, BuildBvTree: true}
		var data []byte
		var size int
		if !detour.DtCreateNavMeshData(&p, &data, &size) {
			t.Fatal("runtime tile conversion failed")
		}
		nav := detour.DtAllocNavMesh()
		if s := nav.Init2(data, size, 0); detour.DtStatusFailed(s) {
			t.Fatalf("tile load: %x", s)
		}
		q := detour.DtAllocNavMeshQuery()
		if s := q.Init(nav, 2048); detour.DtStatusFailed(s) {
			t.Fatalf("query init: %x", s)
		}
		f := detour.DtAllocDtQueryFilter()
		refs := make([]detour.DtPolyRef, 2)
		points := [][]float32{make([]float32, 3), make([]float32, 3)}
		for i, y := range []float32{0, 3} {
			if s := q.FindNearestPoly([]float32{0, y, 0}, []float32{.2, .3, .2}, f, &refs[i], points[i]); detour.DtStatusFailed(s) || refs[i] == 0 {
				t.Fatalf("projection %d: %x", i, s)
			}
			if math.Abs(float64(points[i][1]-y)) > .21 {
				t.Fatalf("selected wrong floor: %v for height %f", points[i], y)
			}
		}
		if refs[0] == refs[1] {
			t.Fatal("stacked surfaces merged")
		}
		path := make([]detour.DtPolyRef, 64)
		var count int
		s := q.FindPath(refs[0], refs[1], points[0], points[1], f, path, &count, len(path))
		if detour.DtStatusFailed(s) || !detour.DtStatusDetail(s, detour.DT_PARTIAL_RESULT) || count < 1 || path[count-1] == refs[1] {
			t.Fatalf("invented bridge connection: status=%x path=%v", s, path[:count])
		}
	}
}
