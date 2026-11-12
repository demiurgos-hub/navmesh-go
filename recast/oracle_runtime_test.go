package recast

import (
	"math"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

type runtimeOracleProbe struct {
	Start, End, Extents [3]float32
	BoundaryHeight      bool
}

type runtimeOracleResult struct {
	StartFound, EndFound                              bool
	Start, End                                        [3]float32
	PathStatus, SlicedStatus, RayStatus, HeightStatus uint32
	Complete, Partial, Hit                            bool
	Height, Fraction                                  float32
}

func runtimeOracleProbes(scene string) []runtimeOracleProbe {
	if scene == "step-above" {
		return []runtimeOracleProbe{
			{Start: [3]float32{4, 1.25, 4}, End: [3]float32{6, 1.25, 10}, Extents: [3]float32{.2, .3, .2}},
			{Start: [3]float32{4, 1.25, 8}, End: [3]float32{20, 1.25, 8}, Extents: [3]float32{.2, .3, .2}},
			{Start: [3]float32{5, 1.25, 8}, End: [3]float32{11, 2, 8}, Extents: [3]float32{.2, .3, .2}},
			{Start: [3]float32{11, 2, 4}, End: [3]float32{11, 2, 10}, Extents: [3]float32{.2, .3, .2}},
		}
	}
	if scene == "radius-safe" || scene == "radius-thin" {
		return []runtimeOracleProbe{
			{Start: [3]float32{8.125, 1.25, 4}, End: [3]float32{8.125, 1.25, 12}, Extents: [3]float32{.1, .3, .1}},
			{Start: [3]float32{8.125, 1.25, 4}, End: [3]float32{8.125, 1.25, 20}, Extents: [3]float32{.1, .3, .1}},
			{Start: [3]float32{4, 1.25, 4}, End: [3]float32{8.125, 1.25, 4}, Extents: [3]float32{.1, .3, .1}},
			{Start: [3]float32{8.125, 1.25, 6}, End: [3]float32{8.125, 1.25, 8}, Extents: [3]float32{.1, .3, .1}},
		}
	}
	p := []runtimeOracleProbe{
		{Start: [3]float32{-4, .1, -4}, End: [3]float32{4, .1, -4}},
		{Start: [3]float32{-4, .1, 0}, End: [3]float32{10, .1, 0}},
		{Start: [3]float32{0, .1, 0}, End: [3]float32{0, 3.1, 0}},
		{Start: [3]float32{2, .1, 3.5}, End: [3]float32{3.5, .1, 3.5}},
	}
	for i := range p {
		p[i].Extents = [3]float32{.2, .3, .2}
		if scene == "slope" {
			// Ordinary probes use cell interiors. An explicit edge case below
			// records the current height-membership difference separately.
			p[i].Start[0] += .125
			p[i].End[0] += .125
			p[i].Start[1] += .25*p[i].Start[0] + 1.5
			p[i].End[1] += .25*p[i].End[0] + 1.5
		}
	}
	if scene == "slope" {
		p = append(p, runtimeOracleProbe{Start: [3]float32{-4, .6, -4}, End: [3]float32{4, 2.6, -4}, Extents: [3]float32{.2, .3, .2}, BoundaryHeight: true})
	}
	return p
}

func runRuntimeOracle(t *testing.T, c Config, r *BuildResult, probes []runtimeOracleProbe) []runtimeOracleResult {
	t.Helper()
	m, d := r.Mesh, r.Detail
	if m.Npolys == 0 {
		return make([]runtimeOracleResult, len(probes))
	}
	flags := make([]uint16, m.Npolys)
	for i := range flags {
		flags[i] = 1
	}
	p := detour.DtNavMeshCreateParams{
		Verts: m.Verts, VertCount: int32(m.Nverts), Polys: m.Polys, PolyFlags: flags,
		PolyAreas: m.Areas, PolyCount: int32(m.Npolys), Nvp: int32(m.Nvp),
		DetailMeshes: d.Meshes, DetailVerts: d.Verts, DetailVertsCount: int32(d.Nverts),
		DetailTris: d.Tris, DetailTriCount: int32(d.Ntris), Bmin: m.Bmin, Bmax: m.Bmax,
		WalkableHeight: float32(c.WalkableHeight) * c.Ch,
		WalkableRadius: float32(c.WalkableRadius) * c.Cs,
		WalkableClimb:  float32(c.WalkableClimb) * c.Ch, Cs: c.Cs, Ch: c.Ch, BuildBvTree: true,
	}
	var data []byte
	var size int
	if !detour.DtCreateNavMeshData(&p, &data, &size) {
		t.Fatal("oracle Go tile conversion failed")
	}
	nav := detour.DtAllocNavMesh()
	if s := nav.Init2(data, size, 0); detour.DtStatusFailed(s) {
		t.Fatalf("oracle Go load: %x", s)
	}
	q := detour.DtAllocNavMeshQuery()
	if s := q.Init(nav, 2048); detour.DtStatusFailed(s) {
		t.Fatalf("oracle Go query init: %x", s)
	}
	f := detour.DtAllocDtQueryFilter()
	out := make([]runtimeOracleResult, len(probes))
	for i, p := range probes {
		r := &out[i]
		var a, b detour.DtPolyRef
		q.FindNearestPoly(p.Start[:], p.Extents[:], f, &a, r.Start[:])
		q.FindNearestPoly(p.End[:], p.Extents[:], f, &b, r.End[:])
		r.StartFound, r.EndFound = a != 0, b != 0
		if a != 0 {
			r.HeightStatus = uint32(q.GetPolyHeight(a, r.Start[:], &r.Height))
			var fraction float32
			var normal [3]float32
			var count int
			visited := make([]detour.DtPolyRef, 64)
			r.RayStatus = uint32(q.Raycast(a, r.Start[:], p.End[:], f, &fraction, normal[:], visited, &count, len(visited)))
			r.Hit = fraction < 1
			r.Fraction = float32(math.Min(float64(fraction), 1))
		}
		if a != 0 && b != 0 {
			path := make([]detour.DtPolyRef, 64)
			var count int
			s := q.FindPath(a, b, r.Start[:], r.End[:], f, path, &count, len(path))
			r.PathStatus = uint32(s)
			r.Complete = count > 0 && path[count-1] == b && !detour.DtStatusDetail(s, detour.DT_PARTIAL_RESULT|detour.DT_BUFFER_TOO_SMALL|detour.DT_OUT_OF_NODES)
			r.Partial = detour.DtStatusDetail(s, detour.DT_PARTIAL_RESULT)
			s = q.InitSlicedFindPath(a, b, r.Start[:], r.End[:], f, 0)
			for n := 0; detour.DtStatusInProgress(s); n++ {
				if n > 2048 {
					t.Fatal("sliced path did not terminate")
				}
				s = q.UpdateSlicedFindPath(8, nil)
			}
			if !detour.DtStatusFailed(s) {
				s = q.FinalizeSlicedFindPath(path, &count, len(path))
			}
			r.SlicedStatus = uint32(s)
		}
	}
	return out
}

func compareRuntimeOracle(t *testing.T, got, want []runtimeOracleResult, probes []runtimeOracleProbe, requirePath bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("runtime probe count: Go=%d C++=%d", len(got), len(want))
	}
	for i, a := range got {
		b := want[i]
		heightStatusMatches := a.HeightStatus == b.HeightStatus
		if probes[i].BoundaryHeight {
			// The current C++ pin uses strict point-in-polygon membership;
			// the Go query includes detail-triangle edges and checks a bounded
			// coordinate-ULP margin for rounded polygon edges. Preserve this
			// selected-polygon boundary-height contract explicitly.
			if a.HeightStatus != uint32(detour.DT_SUCCESS) || b.HeightStatus != uint32(detour.DT_FAILURE|detour.DT_INVALID_PARAM) || math.Abs(float64(a.Height-a.Start[1])) > 1e-4 {
				t.Fatalf("boundary height deviation changed; review contract: Go=%+v C++=%+v", a, b)
			}
			t.Log("documented polygon-edge deviation: Go height succeeds; pinned C++ rejects strict polygon membership")
			heightStatusMatches = true
		}
		if a.StartFound != b.StartFound || a.EndFound != b.EndFound || a.PathStatus != b.PathStatus || a.SlicedStatus != b.SlicedStatus || a.RayStatus != b.RayStatus || !heightStatusMatches || a.Complete != b.Complete || a.Partial != b.Partial || a.Hit != b.Hit {
			t.Fatalf("runtime probe %d outcome: Go=%+v C++=%+v", i, a, b)
		}
		if !probes[i].BoundaryHeight && math.Abs(float64(a.Height-b.Height)) > 1e-4 || math.Abs(float64(a.Fraction-b.Fraction)) > 1e-4 {
			t.Fatalf("runtime probe %d height/raycast: Go=%+v C++=%+v", i, a, b)
		}
		for axis := 0; axis < 3; axis++ {
			if math.Abs(float64(a.Start[axis]-b.Start[axis])) > 1e-4 || math.Abs(float64(a.End[axis]-b.End[axis])) > 1e-4 {
				t.Fatalf("runtime probe %d projection: Go=%+v C++=%+v", i, a, b)
			}
		}
	}
	// Ensure the data exercises a successful route and a missed surface as well
	// as comparing implementations; missing oracle records cannot silently pass.
	if requirePath && (len(want) == 0 || !want[0].Complete) {
		t.Fatalf("reference probes do not exercise a complete route: %+v", want)
	}
	if requirePath && len(want) >= 4 {
		missingWall := false
		for _, probe := range want {
			missingWall = missingWall || !probe.EndFound && probe.Hit
		}
		if !missingWall {
			t.Fatalf("reference probes do not exercise missing/wall cases: %+v", want)
		}
	}
}
