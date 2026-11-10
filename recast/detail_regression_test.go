package recast

import (
	"context"
	"errors"
	"math"
	"testing"
)

// checkedCancellation makes cancellation during stage work deterministic,
// without depending on wall-clock timing or a concurrently scheduled goroutine.
type checkedCancellation struct {
	context.Context
	cancel        context.CancelFunc
	checks, limit int
}

func TestCircumCircleIsEquidistant(t *testing.T) {
	for _, origin := range [][3]float32{{}, {-17, 5, 31}} {
		a, b, d := origin, origin, origin
		b[0] += 2
		d[2] += 2
		for _, reverse := range []bool{false, true} {
			if reverse {
				b, d = d, b
			}
			center, radius, ok := circumCircle(a, b, d)
			if !ok || math.Abs(float64(center[0]-origin[0]-1)) > 1e-6 || math.Abs(float64(center[2]-origin[2]-1)) > 1e-6 {
				t.Fatalf("invalid circumcenter: center=%v radius=%v", center, radius)
			}
			for _, v := range [][3]float32{a, b, d} {
				if math.Abs(float64(vdist2(center, v)-radius)) > 1e-6 {
					t.Fatalf("circumcircle does not contain every defining point: center=%v point=%v radius=%v", center, v, radius)
				}
			}
		}
	}
}

func TestDelaunayHullCoversConvexInterior(t *testing.T) {
	points := []float32{-1, 0, -1, -1, 0, 1, 1, 0, 1, 1, 0, -1, -.4, 0, -.3, .6, 0, -.2, .3, 0, .6, -.5, 0, .4, 0, 0, 0}
	var tris, edges []int
	delaunayHull(9, points, 4, []int{0, 1, 2, 3}, &tris, &edges)
	if len(tris)/4 != 12 { // Euler: 2*n - 2 - hull vertices, no collinear points.
		t.Fatalf("triangulation has %d faces, want 12", len(tris)/4)
	}
	area := float64(0)
	for i := 0; i < len(tris); i += 4 {
		a, b, c := Vcopy(points[tris[i]*3:]), Vcopy(points[tris[i+1]*3:]), Vcopy(points[tris[i+2]*3:])
		area += math.Abs(float64(vcross2(a, b, c))) * .5
	}
	if math.Abs(area-4) > 1e-5 {
		t.Fatalf("triangulation coverage area=%g, want 4", area)
	}
}

func TestBuildDetailRefinementOnWavySurface(t *testing.T) {
	var vertices []float32
	var triangles []int
	for z := 0; z <= 12; z++ {
		for x := 0; x <= 12; x++ {
			fx, fz := float64(x-6), float64(z-6)
			vertices = append(vertices, float32(fx), float32(oracleWaveHeight(fx, fz)), float32(fz))
		}
	}
	for z := 0; z < 12; z++ {
		for x := 0; x < 12; x++ {
			a := z*13 + x
			triangles = append(triangles, a, a+13, a+14, a, a+14, a+1)
		}
	}
	cfg := buildTestConfig()
	cfg.DetailSampleDist, cfg.DetailSampleMaxError = .75, .03
	sourceHeight := func(x, z float32) float64 {
		ix, iz := int(math.Floor(float64(x+6))), int(math.Floor(float64(z+6)))
		u, v := float64(x+6)-float64(ix), float64(z+6)-float64(iz)
		a := iz*13 + ix
		h00, h01 := float64(vertices[a*3+1]), float64(vertices[(a+13)*3+1])
		h11, h10 := float64(vertices[(a+14)*3+1]), float64(vertices[(a+1)*3+1])
		if v >= u {
			return (1-v)*h00 + (v-u)*h01 + u*h11
		}
		return (1-u)*h00 + v*h11 + (u-v)*h10
	}
	// Compare against resolved input triangles. Rasterization samples a cell's
	// footprint, whose source-height variation is bounded by this terrain's
	// |dY/dX|<=.35 and |dY/dZ|<=.30. Add two vertical-cell offsets and the
	// configured detail approximation error. The C++ differential tolerance
	// remains 0.0001; this separate bound measures error relative to raw source.
	heightTolerance := float64(2*cfg.Ch+cfg.DetailSampleMaxError) + float64(cfg.Cs)*(.35+.30) + 1e-5
	for _, mode := range []Partition{Monotone, Watershed, LayerRegions} {
		r, err := Build(context.Background(), cfg, vertices, triangles, nil, BuildOptions{Partition: mode, BuildLayers: true})
		if err != nil {
			t.Fatalf("partition %d: %v", mode, err)
		}
		if r.Detail.Nverts <= r.Mesh.Npolys*r.Mesh.Nvp || r.Detail.Ntris <= r.Mesh.Npolys*(r.Mesh.Nvp-2) {
			t.Fatalf("partition %d did not exercise interior detail refinement", mode)
		}
		for z := float32(-4.875); z < 5; z += .5 {
			for x := float32(-4.875); x < 5; x += .5 {
				samples := detailSamples(r.Detail, r.Mesh.Areas, x, z)
				if len(samples) != 1 || math.Abs(samples[0].y-sourceHeight(x, z)) > heightTolerance {
					t.Fatalf("partition %d incorrect wavy surface at (%g,%g): %v", mode, x, z, samples)
				}
			}
		}
	}
}

func TestDetailSamplingZeroAndTinyDistance(t *testing.T) {
	var vertices []float32
	var triangles []int
	addTestFloor(&vertices, &triangles, -6, -6, 6, 6, 0)
	for _, distance := range []float32{0, 1e-30} {
		name := "zero"
		if distance != 0 {
			name = "tiny"
		}
		t.Run(name, func(t *testing.T) {
			cfg := buildTestConfig()
			cfg.DetailSampleDist = distance
			r, err := Build(context.Background(), cfg, vertices, triangles, nil, BuildOptions{})
			if distance == 0 {
				if err != nil || r == nil || r.Mesh.Npolys == 0 || r.Detail.Ntris == 0 {
					t.Fatalf("disabled detail sampling did not produce base triangulation: %v", err)
				}
				for p := 0; p < r.Mesh.Npolys; p++ {
					nv := 0
					for j := 0; j < r.Mesh.Nvp && r.Mesh.Polys[p*r.Mesh.Nvp*2+j] != MeshNullIdx; j++ {
						nv++
					}
					if r.Detail.Meshes[p*4+1] != uint32(nv) || r.Detail.Meshes[p*4+3] != uint32(nv-2) {
						t.Fatal("disabled sampling changed original hull or omitted base triangles")
					}
				}
			} else if !errors.Is(err, ErrCapacity) {
				t.Fatalf("unrepresentable sample grid must return capacity error: %v", err)
			}
		})
	}
}

func TestDetailSamplingBudgetAndCheckedHeightSearch(t *testing.T) {
	var vertices []float32
	var triangles []int
	addTestFloor(&vertices, &triangles, -6, -6, 6, 6, 0)
	for _, tc := range []struct {
		name       string
		limit      int
		errorLimit float32
		want       error
	}{
		{"candidate-budget", 1, 1.3, ErrCapacity},
		{"negative-budget", -1, 1.3, ErrInvalidInput},
		{"unrepresentable-height-search", 0, math.MaxFloat32, ErrCapacity},
		{"height-search-one-over-limit", 0, maxDetailHeightSearchRadius + 1, ErrCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := buildTestConfig()
			cfg.MaxSimplificationError = tc.errorLimit
			r, err := Build(context.Background(), cfg, vertices, triangles, nil, BuildOptions{MaxDetailSamples: tc.limit})
			if r != nil || !errors.Is(err, tc.want) {
				t.Fatalf("expected no result and %v, got %v", tc.want, err)
			}
		})
	}
	// Charges accumulate across polygons. A later request cannot re-use the
	// full build budget, and a negative-origin grid has the same cost.
	budget := newDetailSamplingBudget(8)
	ctx := context.WithValue(context.Background(), detailSamplingKey{}, budget)
	for i := 0; i < 3; i++ {
		_, _, _, _, ok := detailSampleGrid(ctx, [3]float32{-1, 0, -1}, [3]float32{1, 0, 1}, 1)
		if ok != (i < 2) {
			t.Fatalf("candidate accounting failed at polygon %d", i)
		}
	}
	if !budget.exceeded || budget.remaining != 0 {
		t.Fatal("candidate budget did not retain exhaustion")
	}
}

func TestDetailHeightSearchCancellation(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &checkedCancellation{Context: base, cancel: cancel, limit: 1}
	hp := &HeightPatch{width: 1, height: 1, data: []uint16{unsetHeight}}
	if got := getHeight(ctx, 0, 0, 0, 1, 1, 1, maxDetailHeightSearchRadius, hp); got != unsetHeight || base.Err() != context.Canceled {
		t.Fatal("large height search did not observe cancellation")
	}
}

func TestDetailCandidateAndRefinementCancellation(t *testing.T) {
	// Flat edges need no inserted vertices. The 100x100 interior grid reaches
	// both candidate collection and the refinement scan; cancellation checks
	// are deterministic, independent of wall-clock scheduling.
	for _, tc := range []struct {
		name  string
		limit int
	}{{"candidate-collection", 8}, {"refinement-scan", 207}} {
		t.Run(tc.name, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &checkedCancellation{Context: base, cancel: cancel, limit: tc.limit}
			hp := &HeightPatch{width: 11, height: 11, data: make([]uint16, 121)}
			chf := &CompactHeightfield{Cs: 1, Ch: 1}
			poly := []float32{0, 0, 0, 0, 0, 10, 10, 0, 10, 10, 0, 0}
			var nverts int
			var tris, edges, samples []int
			buildPolyDetail(ctx, poly, 4, .1, .1, 1, chf, hp, make([]float32, 127*3), &nverts, &tris, &edges, &samples)
			if base.Err() != context.Canceled || nverts != 0 || tris != nil {
				t.Fatalf("detail work published output after cancellation: checks=%d vertices=%d triangles=%d", ctx.checks, nverts, len(tris)/4)
			}
		})
	}
}

func (c *checkedCancellation) Err() error {
	c.checks++
	if c.checks > c.limit {
		c.cancel()
	}
	return c.Context.Err()
}

func TestDetailHeightPatchFullQueueBatchAndCancellation(t *testing.T) {
	for _, cancelDuringWork := range []bool{false, true} {
		chf := &CompactHeightfield{Width: 256, Height: 2, Cells: make([]CompactCell, 512), Spans: make([]CompactSpan, 512)}
		for i := range chf.Spans {
			chf.Cells[i] = CompactCell{Index: uint32(i), Count: 1}
			chf.Spans[i] = CompactSpan{Y: 5, Reg: uint16(i/256 + 1)}
			for dir := 0; dir < 4; dir++ {
				SetCon(&chf.Spans[i], dir, notConnected)
			}
			if i < 256 {
				SetCon(&chf.Spans[i], 1, 0)
			} else {
				SetCon(&chf.Spans[i], 3, 0)
			}
		}
		hp := &HeightPatch{width: 256, height: 1, data: make([]uint16, 256)}
		base, cancel := context.WithCancel(context.Background())
		ctx := &checkedCancellation{Context: base, cancel: cancel, limit: 8}
		if cancelDuringWork {
			ctx.limit = 1 // After the row scan, while entering height flood fill.
		}
		var queue []int
		getHeightData(ctx, chf, nil, 0, nil, 0, hp, &queue, 1)
		if cancelDuringWork {
			if !errors.Is(base.Err(), context.Canceled) {
				t.Fatal("height flood fill ignored cancellation")
			}
		} else if base.Err() != nil || len(queue) != 0 {
			t.Fatalf("exact 256-entry batch did not drain: remaining=%d checks=%d error=%v", len(queue)/3, ctx.checks, base.Err())
		}
		cancel()
	}
}
