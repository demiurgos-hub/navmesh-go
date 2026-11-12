package navmesh

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func capturedMovementSnapshot(t testing.TB) *Snapshot {
	t.Helper()
	data, err := os.ReadFile("testdata/movement-detail-height.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle artifact.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	s, err := NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func compareMovementSample(t *testing.T, q *Query, ref detour.DtPolyRef, tile *detour.DtMeshTile, poly *detour.DtPoly, position [3]float32) {
	t.Helper()
	var wantHeight float32
	wantStatus := q.query.GetPolyHeight(ref, position[:], &wantHeight)
	wantPosition := position
	wantPosition[1] = wantHeight
	// These are the separate implementations used before the fused sampler.
	wantNormal := q.normal(ref, wantPosition)
	height, normal, status := q.sampleMovementSurface(ref, tile, poly, position)
	if status != wantStatus {
		t.Fatalf("ref=%d position=%v: status=%v, want %v", ref, position, status, wantStatus)
	}
	if detour.DtStatusFailed(status) {
		return // Failed samples do not expose a height or normal through Move.
	}
	if math.Float32bits(height) != math.Float32bits(wantHeight) || math.Float64bits(normal.X) != math.Float64bits(wantNormal.X) || math.Float64bits(normal.Y) != math.Float64bits(wantNormal.Y) || math.Float64bits(normal.Z) != math.Float64bits(wantNormal.Z) {
		t.Fatalf("ref=%d position=%v: height=%v normal=%v, want height=%v normal=%v", ref, position, height, normal, wantHeight, wantNormal)
	}
}

func TestMovementSamplerKeepsIndependentTriangleChoices(t *testing.T) {
	s, _, _ := scratchChainSnapshot(t, 1, false, false)
	tile := s.mesh.GetTile(0)
	poly := &tile.Polys[0]
	ref := s.mesh.GetPolyRefBase(tile)
	// The first triangle is flat. The second rises to the fourth vertex.
	// Just across the diagonal, strict float32 height selects triangle two,
	// while the original tolerant normal predicate still selects triangle one.
	tile.Verts[3*3+1] = 1
	tile.DetailTris = []uint8{0, 1, 2, 0, 0, 2, 3, 0}
	tile.DetailMeshes[0].TriBase = 0
	tile.DetailMeshes[0].TriCount = 2
	q := transferQuery(t, s, Limits{})
	position := [3]float32{1, 0, 1 - 1e-6}
	height, normal, status := q.sampleMovementSurface(ref, tile, poly, position)
	if detour.DtStatusFailed(status) || height <= 0 || normal != (Vec3{Y: 1}) {
		t.Fatalf("independent triangle choices were lost: height=%v normal=%v status=%v", height, normal, status)
	}
	for _, point := range [][3]float32{position, {1, 100, 1}, {1, -100, 1 + 1e-6}, {0, 0, 0}, {2, 0, 2}, {1, 0, -1e-7}, {1, 0, -1}, {1, 0, 1.5}} {
		compareMovementSample(t, q, ref, tile, poly, point)
	}
	// Degenerate detail must still use the unchanged selected-poly fallback;
	// missing normal support remains zero rather than selecting another floor.
	tile.DetailTris = []uint8{0, 0, 0, 0}
	tile.DetailMeshes[0].TriCount = 1
	compareMovementSample(t, q, ref, tile, poly, [3]float32{1, 0, 1})
}

func TestMovementSamplerExactlyMatchesSeparateScans(t *testing.T) {
	scene, cfg := testworld.Scene()
	scene.Links = []geom.Link{{ID: "stairs", Revision: 1, Start: Vec3{X: -3, Y: .1}, End: Vec3{X: -3, Y: 3.1}, Radius: .4, Area: 2, Flags: 1, Bidirectional: true}}
	built, err := bake.Build(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	stacked, err := NewSnapshot(built.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	ground, offmesh := 0, 0
	for _, snapshot := range []*Snapshot{stacked, capturedMovementSnapshot(t)} {
		q := transferQuery(t, snapshot, Limits{})
		for i := int32(0); i < snapshot.mesh.GetMaxTiles(); i++ {
			tile := snapshot.mesh.GetTile(int(i))
			if tile == nil || tile.Header == nil {
				continue
			}
			for j := range tile.Polys {
				poly := &tile.Polys[j]
				ref := snapshot.mesh.GetPolyRefBase(tile) + detour.DtPolyRef(j)
				if poly.GetType() != detour.DT_POLYTYPE_GROUND {
					offmesh++
					vertex := tile.Verts[int(poly.Verts[0])*3:]
					compareMovementSample(t, q, ref, tile, poly, [3]float32{vertex[0] + 1, vertex[1], vertex[2]})
					continue
				}
				ground++
				detail := tile.DetailMeshes[j]
				vertex := func(index uint8) [3]float32 {
					var data []float32
					if index < poly.VertCount {
						data = tile.Verts[int(poly.Verts[index])*3:]
					} else {
						data = tile.DetailVerts[(int(detail.VertBase)+int(index)-int(poly.VertCount))*3:]
					}
					return [3]float32{data[0], data[1], data[2]}
				}
				for k := uint32(0); k < uint32(detail.TriCount); k++ {
					tri := tile.DetailTris[(detail.TriBase+k)*4:]
					a, b, c := vertex(tri[0]), vertex(tri[1]), vertex(tri[2])
					points := [][3]float32{a, b, c, {(a[0] + b[0] + c[0]) / 3, 0, (a[2] + b[2] + c[2]) / 3}, {(a[0] + b[0]) / 2, 0, (a[2] + b[2]) / 2}, {(b[0] + c[0]) / 2, 0, (b[2] + c[2]) / 2}, {(c[0] + a[0]) / 2, 0, (c[2] + a[2]) / 2}}
					for _, point := range points {
						compareMovementSample(t, q, ref, tile, poly, point)
						for _, axis := range []int{0, 2} {
							for _, direction := range []float32{float32(math.Inf(-1)), float32(math.Inf(1))} {
								rounded := point
								rounded[axis] = math.Nextafter32(rounded[axis], direction)
								compareMovementSample(t, q, ref, tile, poly, rounded)
							}
						}
					}
				}
			}
		}
	}
	if ground == 0 || offmesh == 0 {
		t.Fatalf("fixture coverage missing: ground=%d offmesh=%d", ground, offmesh)
	}
}

var movementSampleHeight float32
var movementSampleNormal Vec3
var movementSampleStatus detour.DtStatus

func BenchmarkMovementSurfaceSampling(b *testing.B) {
	s := capturedMovementSnapshot(b)
	q, err := s.NewQuery(Limits{})
	if err != nil {
		b.Fatal(err)
	}
	start := q.Project(context.Background(), Vec3{X: 95.76627349853516, Y: .4000493884086609, Z: 92.9614486694336}, ProjectionOptions{Horizontal: .2, Up: .2, Down: .2}, Filter{})
	if start.Code != Complete {
		b.Fatal(start.Status)
	}
	ref := start.Surface.ref
	var tile *detour.DtMeshTile
	var poly *detour.DtPoly
	s.mesh.GetTileAndPolyByRef(ref, &tile, &poly)
	pos := q.local(start.Surface.position)
	b.Run("Separate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			movementSampleStatus = q.query.GetPolyHeight(ref, pos[:], &movementSampleHeight)
			movementSampleNormal = q.normal(ref, pos)
		}
	})
	b.Run("Combined", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			movementSampleHeight, movementSampleNormal, movementSampleStatus = q.sampleMovementSurface(ref, tile, poly, pos)
		}
	})
}
