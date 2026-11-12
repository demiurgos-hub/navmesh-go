package recast

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestRemoveBoundaryVertexPreservesWrapEdge(t *testing.T) {
	for _, firstVertex := range []bool{false, true} {
		for rotation := 0; rotation < 3; rotation++ {
			t.Run(fmt.Sprintf("first=%v/rotation=%d", firstVertex, rotation), func(t *testing.T) {
				// Three triangles fill a rectangle, with one redundant vertex
				// on its top edge. Unused polygon slots are null sentinels.
				verts := [][3]uint16{{0, 0, 0}, {0, 0, 2}, {1, 0, 2}, {2, 0, 2}, {2, 0, 0}}
				indices := [3][3]uint16{{0, 1, 2}, {0, 2, 4}, {2, 3, 4}}
				rem := uint16(2)
				if firstVertex {
					verts[0], verts[2] = verts[2], verts[0]
					rem = 0
					for i := range indices {
						for j, index := range indices[i] {
							if index == 0 {
								indices[i][j] = 2
							} else if index == 2 {
								indices[i][j] = 0
							}
						}
					}
				}
				mesh := &PolyMesh{Nverts: 5, Npolys: 3, Nvp: 6, MaxPolys: 3, Polys: make([]uint16, 36), Regs: []uint16{7, 7, 7}, Areas: []uint8{2, 2, 2}}
				for _, v := range verts {
					mesh.Verts = append(mesh.Verts, v[:]...)
				}
				for i := range mesh.Polys {
					mesh.Polys[i] = meshNullIdx
				}
				for i, tri := range indices {
					for j := 0; j < 3; j++ {
						mesh.Polys[i*12+j] = tri[(j+rotation)%3]
					}
				}
				if !canRemoveVertex(mesh, rem) {
					t.Fatal("redundant boundary vertex incorrectly retained")
				}
				if !removeVertex(mesh, rem, 3) || mesh.Nverts != 4 || mesh.Npolys != 1 {
					t.Fatalf("rectangle reconstruction failed: vertices=%d polygons=%d", mesh.Nverts, mesh.Npolys)
				}
				p := mesh.Polys[:6]
				if countPolyVerts(p, 6) != 4 || mesh.Regs[0] != 7 || mesh.Areas[0] != 2 {
					t.Fatalf("polygon metadata changed: %v", p)
				}
				area := 0
				for j := 0; j < 4; j++ {
					if int(p[j]) >= mesh.Nverts {
						t.Fatalf("sentinel or stale vertex entered polygon: %v", p)
					}
					a, b := int(p[j])*3, int(p[(j+1)%4])*3
					area += int(mesh.Verts[a])*int(mesh.Verts[b+2]) - int(mesh.Verts[b])*int(mesh.Verts[a+2])
				}
				if area != -8 {
					t.Fatalf("wrap edge or winding changed: twice signed area=%d", area)
				}
			})
		}
	}
}

type terrainBorderTile struct {
	Config    Config
	Vertices  []float32
	Triangles []int
	Areas     []uint8
	Options   BuildOptions
}

func readTerrainBorderTile(t *testing.T) terrainBorderTile {
	t.Helper()
	data, err := os.ReadFile("testdata/terrain-border-tile.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture terrainBorderTile
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestTerrainBorderVertexRegression(t *testing.T) {
	// Resolved column (-7,-7) from the deterministic terrain generator, seed 1:
	// 74 triangles with the original padding, roof, areas, and physical profile.
	// The old postincrement translation admitted the null sentinel into a hole.
	f := readTerrainBorderTile(t)
	for _, partition := range []Partition{Monotone, Watershed, LayerRegions} {
		t.Run(fmt.Sprint(partition), func(t *testing.T) {
			options := f.Options
			options.Partition = partition
			built, err := Build(context.Background(), f.Config, f.Vertices, f.Triangles, f.Areas, options)
			if err != nil {
				t.Fatal(err)
			}
			if built.Mesh.Nverts == 0 || built.Mesh.Npolys == 0 || built.Detail.Nmeshes != built.Mesh.Npolys {
				t.Fatal("terrain tile lost usable geometry")
			}
		})
	}
}
