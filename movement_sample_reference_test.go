package navmesh

import (
	"math"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

// Preserve the previous independent normal scan as the exact-output reference
// for fused sampling and full-traversal movement parity tests.
func (q *Query) normal(ref detour.DtPolyRef, pos [3]float32) Vec3 {
	var tile *detour.DtMeshTile
	var poly *detour.DtPoly
	if detour.DtStatusFailed(q.snapshot.mesh.GetTileAndPolyByRef(ref, &tile, &poly)) || poly.GetType() != detour.DT_POLYTYPE_GROUND {
		return Vec3{}
	}
	index := int(q.snapshot.mesh.DecodePolyIdPoly(ref))
	if index >= len(tile.DetailMeshes) {
		return Vec3{}
	}
	detail := tile.DetailMeshes[index]
	vertex := func(i uint8) Vec3 {
		var v []float32
		if i < poly.VertCount {
			idx := int(poly.Verts[i]) * 3
			v = tile.Verts[idx : idx+3]
		} else {
			idx := (int(detail.VertBase) + int(i) - int(poly.VertCount)) * 3
			v = tile.DetailVerts[idx : idx+3]
		}
		return Vec3{X: float64(v[0]), Y: float64(v[1]), Z: float64(v[2])}
	}
	for i := uint32(0); i < uint32(detail.TriCount); i++ {
		tri := tile.DetailTris[(detail.TriBase+i)*4:]
		a, b, c := vertex(tri[0]), vertex(tri[1]), vertex(tri[2])
		ux, uz := b.X-a.X, b.Z-a.Z
		vx, vz := c.X-a.X, c.Z-a.Z
		dx, dz := float64(pos[0])-a.X, float64(pos[2])-a.Z
		den := ux*vz - uz*vx
		if math.Abs(den) < 1e-15 {
			continue
		}
		u, v := (dx*vz-dz*vx)/den, (ux*dz-uz*dx)/den
		if u < -1e-5 || v < -1e-5 || u+v > 1+1e-5 {
			continue
		}
		uy, vy := b.Y-a.Y, c.Y-a.Y
		n := Vec3{X: uy*vz - uz*vy, Y: uz*vx - ux*vz, Z: ux*vy - uy*vx}
		length := math.Sqrt(n.X*n.X + n.Y*n.Y + n.Z*n.Z)
		if length == 0 {
			continue
		}
		if n.Y < 0 {
			length = -length
		}
		return Vec3{X: n.X / length, Y: n.Y / length, Z: n.Z / length}
	}
	return Vec3{}
}
