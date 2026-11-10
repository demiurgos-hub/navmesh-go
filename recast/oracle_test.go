package recast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"testing"
)

// This suite intentionally skips unless the separately compiled oracle is
// explicitly supplied. It never downloads source or invokes a compiler.
func TestUpstreamOracle(t *testing.T) {
	exe := os.Getenv("NAVMESH_RECAST_ORACLE")
	if exe == "" {
		t.Skip("set NAVMESH_RECAST_ORACLE to the separately built pinned C++ runner")
	}
	for _, scene := range []string{"floor", "slope", "stacked-hole", "padded-tile", "step-above", "radius-safe", "radius-thin", "wavy-detail"} {
		for _, mode := range []Partition{Monotone, Watershed, LayerRegions} {
			t.Run(fmt.Sprintf("%s/%d", scene, mode), func(t *testing.T) {
				cfg := buildTestConfig()
				var verts []float32
				var tris []int
				addTestFloor(&verts, &tris, -6, -6, 6, 6, 0)
				opt := BuildOptions{Partition: mode, BuildLayers: true}
				if scene == "slope" {
					verts[4] = 3
					verts[7] = 3
				}
				if scene == "stacked-hole" {
					addTestFloor(&verts, &tris, -2, -2, 2, 2, 3)
					opt.Volumes = []AreaVolume{{Shape: BoxArea, Min: [3]float32{3, -1, 3}, Max: [3]float32{4, 1, 4}, Area: 0}}
				}
				if scene == "padded-tile" {
					cfg.BorderSize = 5
					cfg.Width = 58
					cfg.Height = 58
					cfg.Bmin[0] -= 1.25
					cfg.Bmin[2] -= 1.25
					cfg.Bmax[0] += 1.25
					cfg.Bmax[2] += 1.25
				}
				if scene == "step-above" || scene == "radius-safe" || scene == "radius-thin" {
					// Exact resolved inputs from the end-to-end threshold corpus.
					// step-above once hung when height BFS had exactly 256 entries.
					cfg = Config{Width: 72, Height: 72, BorderSize: 4, Cs: .25, Ch: .25, Bmin: [3]float32{-1, 0, -1}, Bmax: [3]float32{17, 17, 17}, WalkableSlopeAngle: 45, WalkableHeight: 6, WalkableClimb: 2, WalkableRadius: 1, MaxEdgeLen: 60, MaxSimplificationError: 1.3, MergeRegionArea: 400, MaxVertsPerPoly: 6, DetailSampleDist: 1.2, DetailSampleMaxError: .1}
					verts, tris = nil, nil
					if scene == "step-above" {
						addTestFloor(&verts, &tris, 2, 2, 8, 14, 1)
						addTestFloor(&verts, &tris, 8, 2, 14, 14, 1.75)
					} else {
						cfg.WalkableRadius = 2
						if scene == "radius-thin" {
							cfg.WalkableRadius = 3
						}
						cfg.BorderSize = cfg.WalkableRadius + 3
						cfg.Width, cfg.Height = 64+2*cfg.BorderSize, 64+2*cfg.BorderSize
						pad := float32(cfg.BorderSize) * cfg.Cs
						cfg.Bmin, cfg.Bmax = [3]float32{-pad, 0, -pad}, [3]float32{16 + pad, 17, 16 + pad}
						addTestFloor(&verts, &tris, 2, 2, 14, 14, 1)
						opt.Volumes = []AreaVolume{{Shape: BoxArea, Min: [3]float32{2, -1, 2}, Max: [3]float32{7, 3, 14}}, {Shape: BoxArea, Min: [3]float32{9, -1, 2}, Max: [3]float32{14, 3, 14}}}
					}
				}
				probes := runtimeOracleProbes(scene)
				if scene == "wavy-detail" {
					verts, tris = nil, nil
					for z := 0; z <= 12; z++ {
						for x := 0; x <= 12; x++ {
							fx, fz := float64(x-6), float64(z-6)
							verts = append(verts, float32(fx), float32(oracleWaveHeight(fx, fz)), float32(fz))
						}
					}
					for z := 0; z < 12; z++ {
						for x := 0; x < 12; x++ {
							a := z*13 + x
							b, c, d := a+13, a+14, a+1
							tris = append(tris, a, b, c, a, c, d)
						}
					}
					cfg.DetailSampleDist = .75
					cfg.DetailSampleMaxError = .03
					probes = []runtimeOracleProbe{{Start: [3]float32{-4.125, 0, -3.125}, End: [3]float32{4.125, 0, 3.125}, Extents: [3]float32{.1, .3, .1}}, {Start: [3]float32{-.125, 0, .125}, End: [3]float32{3.125, 0, -3.125}, Extents: [3]float32{.1, .3, .1}}}
					for i := range probes {
						probes[i].Start[1] = float32(oracleWaveHeight(float64(probes[i].Start[0]), float64(probes[i].Start[2]))) + .1
						probes[i].End[1] = float32(oracleWaveHeight(float64(probes[i].End[0]), float64(probes[i].End[2]))) + .1
					}
				}
				got, goErr := Build(context.Background(), cfg, verts, tris, nil, opt)
				var in bytes.Buffer
				fmt.Fprintf(&in, "1 %d 1\n%d %d %d %.9g %.9g\n", mode, cfg.Width, cfg.Height, cfg.BorderSize, cfg.Cs, cfg.Ch)
				for _, v := range cfg.Bmin {
					fmt.Fprintf(&in, "%.9g ", v)
				}
				for _, v := range cfg.Bmax {
					fmt.Fprintf(&in, "%.9g ", v)
				}
				fmt.Fprintf(&in, "\n%.9g %d %d %d\n%d %.9g %d %d\n%d %.9g %.9g\n%d %d\n", cfg.WalkableSlopeAngle, cfg.WalkableHeight, cfg.WalkableClimb, cfg.WalkableRadius, cfg.MaxEdgeLen, cfg.MaxSimplificationError, cfg.MinRegionArea, cfg.MergeRegionArea, cfg.MaxVertsPerPoly, cfg.DetailSampleDist, cfg.DetailSampleMaxError, len(verts)/3, len(tris)/3)
				for _, v := range verts {
					fmt.Fprintf(&in, "%.9g ", v)
				}
				fmt.Fprintln(&in)
				for _, v := range tris {
					fmt.Fprintf(&in, "%d ", v)
				}
				fmt.Fprintln(&in)
				for i := 0; i < len(tris)/3; i++ {
					fmt.Fprint(&in, "63 ")
				}
				fmt.Fprintln(&in)
				fmt.Fprintln(&in, len(opt.Volumes))
				for _, v := range opt.Volumes {
					fmt.Fprintf(&in, "%d %d ", v.Shape, v.Area)
					for _, x := range v.Min {
						fmt.Fprintf(&in, "%.9g ", x)
					}
					for _, x := range v.Max {
						fmt.Fprintf(&in, "%.9g ", x)
					}
					fmt.Fprintf(&in, "%.9g %.9g %d ", v.Radius, v.Height, len(v.Vertices)/3)
					for _, x := range v.Vertices {
						fmt.Fprintf(&in, "%.9g ", x)
					}
					fmt.Fprintln(&in)
				}
				fmt.Fprintln(&in, len(probes))
				for _, p := range probes {
					for _, point := range [][3]float32{p.Start, p.End, p.Extents} {
						for _, v := range point {
							fmt.Fprintf(&in, "%.9g ", v)
						}
					}
					fmt.Fprintln(&in)
				}
				cmd := exec.Command(exe)
				cmd.Stdin = &in
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("oracle: %v: %s", err, stderr.String())
				}
				var reference struct {
					Reference   string
					Layers      int
					MaxDistance int
					Meshes      []uint32
					Verts       []float32
					Tris        []uint8
					Areas       []uint8
					Probes      []runtimeOracleResult
				}
				if err = json.Unmarshal(out, &reference); err != nil {
					t.Fatalf("oracle output: %v: %s", err, out)
				}
				if reference.Reference != "9f4ce64458dfae86e1239c525ddc219c4e9e06f1" {
					t.Fatal("unexpected oracle revision")
				}
				if goErr != nil {
					t.Fatalf("Go build: %v; C++ detail vertices=%d triangles=%d polygons=%d", goErr, len(reference.Verts)/3, len(reference.Tris)/4, len(reference.Meshes)/4)
				}
				if reference.MaxDistance != got.Stats.MaxDistance {
					t.Fatalf("distance field: Go=%d C++=%d", got.Stats.MaxDistance, reference.MaxDistance)
				}
				if reference.Layers != got.Layers.NLayers {
					t.Fatalf("layer count: go=%d cpp=%d", got.Layers.NLayers, reference.Layers)
				}
				compareRuntimeOracle(t, runRuntimeOracle(t, cfg, got, probes), reference.Probes, probes, scene != "radius-thin")
				if (scene == "stacked-hole" || scene == "step-above") && !reference.Probes[2].Partial {
					t.Fatal("reference stacked-floor probe did not exercise a partial route")
				}
				want := &PolyMeshDetail{Meshes: reference.Meshes, Verts: reference.Verts, Tris: reference.Tris, Nmeshes: len(reference.Meshes) / 4}
				pad := float32(cfg.BorderSize) * cfg.Cs
				for z := cfg.Bmin[2] + pad + .125; z < cfg.Bmax[2]-pad; z += .25 {
					for x := cfg.Bmin[0] + pad + .125; x < cfg.Bmax[0]-pad; x += .25 {
						a := detailSamples(got.Detail, got.Mesh.Areas, x, z)
						b := detailSamples(want, reference.Areas, x, z)
						if len(a) != len(b) {
							t.Fatalf("surface connectivity/coverage differs at (%g,%g): go=%v cpp=%v", x, z, a, b)
						}
						for i := range a {
							if a[i].area != b[i].area || math.Abs(a[i].y-b[i].y) > 1e-4 {
								t.Fatalf("surface differs at (%g,%g): go=%v cpp=%v", x, z, a, b)
							}
						}
					}
				}
			})
		}
	}
}

func oracleWaveHeight(x, z float64) float64 { return 1 + .5*math.Sin(x*.7)*math.Cos(z*.6) }

type surfaceSample struct {
	y    float64
	area uint8
}

func detailSamples(d *PolyMeshDetail, areas []uint8, x, z float32) []surfaceSample {
	var out []surfaceSample
	for i := 0; i < d.Nmeshes; i++ {
		m := d.Meshes[i*4 : i*4+4]
		for j := m[2]; j < m[2]+m[3]; j++ {
			tr := d.Tris[j*4 : j*4+3]
			var v [3][3]float64
			for k := 0; k < 3; k++ {
				n := (int(m[0]) + int(tr[k])) * 3
				for axis := 0; axis < 3; axis++ {
					v[k][axis] = float64(d.Verts[n+axis])
				}
			}
			x0, z0 := v[0][0], v[0][2]
			dx1, dz1 := v[1][0]-x0, v[1][2]-z0
			dx2, dz2 := v[2][0]-x0, v[2][2]-z0
			den := dx1*dz2 - dz1*dx2
			if math.Abs(den) < 1e-12 {
				continue
			}
			px, pz := float64(x)-x0, float64(z)-z0
			u, w := (px*dz2-pz*dx2)/den, (dx1*pz-dz1*px)/den
			if u < -1e-6 || w < -1e-6 || u+w > 1+1e-6 {
				continue
			}
			y := v[0][1] + u*(v[1][1]-v[0][1]) + w*(v[2][1]-v[0][1])
			duplicate := false
			for _, s := range out {
				if math.Abs(s.y-y) < 1e-4 && s.area == areas[i] {
					duplicate = true
					break
				}
			}
			if !duplicate {
				out = append(out, surfaceSample{y, areas[i]})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].y < out[j].y })
	return out
}
