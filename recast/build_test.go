package recast

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func buildTestConfig() Config {
	return Config{Width: 48, Height: 48, Bmin: [3]float32{-6, -1, -6}, Bmax: [3]float32{6, 8, 6}, Cs: .25, Ch: .1, WalkableSlopeAngle: 45, WalkableHeight: 18, WalkableClimb: 3, WalkableRadius: 2, MaxEdgeLen: 12, MaxSimplificationError: 1.3, MaxVertsPerPoly: 6, DetailSampleDist: 1.5, DetailSampleMaxError: .1}
}

func addTestFloor(verts *[]float32, tris *[]int, xmin, zmin, xmax, zmax, y float32) {
	i := len(*verts) / 3
	*verts = append(*verts, xmin, y, zmin, xmax, y, zmin, xmax, y, zmax, xmin, y, zmax)
	*tris = append(*tris, i, i+2, i+1, i, i+3, i+2)
}

func TestBuildAllPartitionsStackedSurfacesAndHole(t *testing.T) {
	var verts []float32
	var tris []int
	addTestFloor(&verts, &tris, -6, -6, 6, 6, 0)
	addTestFloor(&verts, &tris, -2, -2, 2, 2, 3)
	for _, mode := range []Partition{Monotone, Watershed, LayerRegions} {
		t.Run([]string{"monotone", "watershed", "layers"}[mode], func(t *testing.T) {
			r, err := Build(context.Background(), buildTestConfig(), verts, tris, nil, BuildOptions{Partition: mode, BuildLayers: true, Volumes: []AreaVolume{{Shape: BoxArea, Min: [3]float32{3, -1, 3}, Max: [3]float32{4, 1, 4}, Area: 0}}})
			if err != nil {
				t.Fatal(err)
			}
			if r.Mesh.Npolys == 0 || r.Detail.Ntris == 0 || r.Layers.NLayers < 2 {
				t.Fatalf("missing output: polygons=%d details=%d layers=%d", r.Mesh.Npolys, r.Detail.Ntris, r.Layers.NLayers)
			}
			low, high := false, false
			for i := 1; i < len(r.Detail.Verts); i += 3 {
				if r.Detail.Verts[i] < 1 {
					low = true
				}
				if r.Detail.Verts[i] > 2 {
					high = true
				}
			}
			if !low || !high {
				t.Fatal("stacked surface lost")
			}
			if err := ValidateMesh(r.Mesh, r.Detail); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildPreservesAreasAndNeverRestoresExclusions(t *testing.T) {
	var v []float32
	var tr []int
	addTestFloor(&v, &tr, -6, -6, 6, 6, 0)
	c := buildTestConfig()
	r, err := Build(context.Background(), c, v, tr, []uint8{7, 7}, BuildOptions{Volumes: []AreaVolume{{Shape: BoxArea, Min: c.Bmin, Max: c.Bmax, Area: 0}, {Shape: BoxArea, Min: c.Bmin, Max: c.Bmax, Area: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mesh.Npolys != 0 {
		t.Fatal("paint resurrected excluded surface")
	}
	r, err = Build(context.Background(), c, v, tr, []uint8{7, 7}, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range r.Mesh.Areas[:r.Mesh.Npolys] {
		if a != 7 {
			t.Fatalf("area replaced: %d", a)
		}
	}
}

func TestBuildEmptyCancelledAndInvalid(t *testing.T) {
	c := buildTestConfig()
	r, err := Build(context.Background(), c, nil, nil, nil, BuildOptions{BuildLayers: true})
	if err != nil || r.Mesh.Npolys != 0 || r.Layers == nil {
		t.Fatalf("empty: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Build(ctx, c, nil, nil, nil, BuildOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	var v []float32
	var tr []int
	addTestFloor(&v, &tr, -6, -6, 6, 6, 0)
	for _, test := range []struct {
		name   string
		mutate func(*Config, []float32, []int)
		opt    BuildOptions
	}{
		{"nan", func(_ *Config, v []float32, _ []int) { v[0] = float32(math.NaN()) }, BuildOptions{}},
		{"index", func(_ *Config, _ []float32, tr []int) { tr[0] = -1 }, BuildOptions{}},
		{"cell", func(c *Config, _ []float32, _ []int) { c.Cs = 0 }, BuildOptions{}},
		{"span_range", func(c *Config, _ []float32, _ []int) { c.Bmax[1] = 10000 }, BuildOptions{}},
		{"budget", func(_ *Config, _ []float32, _ []int) {}, BuildOptions{MaxCells: 4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cc := c
			vv := append([]float32(nil), v...)
			tt := append([]int(nil), tr...)
			test.mutate(&cc, vv, tt)
			if _, e := Build(context.Background(), cc, vv, tt, nil, test.opt); e == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}

func TestBuildSlopeAndClearance(t *testing.T) {
	c := buildTestConfig()
	var v []float32
	var tr []int
	addTestFloor(&v, &tr, -6, -6, 6, 6, 0)
	// A solid low ceiling removes ground below it, even when nonwalkable.
	addTestFloor(&v, &tr, -6, -6, 6, 6, 1)
	r, err := Build(context.Background(), c, v, tr, []uint8{WalkableArea, WalkableArea, 0, 0}, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mesh.Npolys != 0 {
		t.Fatal("ground beneath low ceiling remained walkable")
	}
	v = nil
	tr = nil
	addTestFloor(&v, &tr, -6, -6, 6, 6, 0)
	v[4] = 3
	v[7] = 3
	r, err = Build(context.Background(), c, v, tr, nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Detail.Ntris == 0 {
		t.Fatal("sloped surface missing")
	}
	minY, maxY := float32(100), float32(-100)
	for i := 1; i < len(r.Detail.Verts); i += 3 {
		minY = MinF(minY, r.Detail.Verts[i])
		maxY = MaxF(maxY, r.Detail.Verts[i])
	}
	if maxY-minY < 2 {
		t.Fatal("detail heights lost slope")
	}
}

func FuzzBuildRejectsMalformedGeometry(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 0, 0, 1, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 192 {
			return
		}
		v := make([]float32, len(data))
		for i, b := range data {
			v[i] = float32(int8(b))
		}
		_, err := Build(context.Background(), buildTestConfig(), v, []int{0, 1, 2}, nil, BuildOptions{MaxCells: 4096, MaxSpans: 4096})
		if err != nil && strings.Contains(err.Error(), "internal invariant") {
			t.Fatal(err)
		}
	})
}
