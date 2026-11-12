package navmesh

import (
	"math"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

// The caller already validated ref and resolved its tile/poly. Height and
// normal deliberately keep independent first-match rules: float32 strict
// containment and float64 tolerant containment can choose different triangles.
func (q *Query) sampleMovementSurface(ref detour.DtPolyRef, tile *detour.DtMeshTile, poly *detour.DtPoly, pos [3]float32) (float32, Vec3, detour.DtStatus) {
	var height float32
	var normal Vec3
	if poly.GetType() != detour.DT_POLYTYPE_GROUND {
		status := q.query.GetPolyHeight(ref, pos[:], &height)
		return height, normal, status
	}
	detail := tile.DetailMeshes[q.snapshot.mesh.DecodePolyIdPoly(ref)]
	heightFound, normalFound := false, false
	for i := uint32(0); i < uint32(detail.TriCount); i++ {
		tri := tile.DetailTris[(detail.TriBase+i)*4:]
		var vertices [3][3]float32
		for j := 0; j < 3; j++ {
			if tri[j] < poly.VertCount {
				index := int(poly.Verts[tri[j]]) * 3
				copy(vertices[j][:], tile.Verts[index:index+3])
			} else {
				index := (int(detail.VertBase) + int(tri[j]) - int(poly.VertCount)) * 3
				copy(vertices[j][:], tile.DetailVerts[index:index+3])
			}
		}
		if !heightFound {
			heightFound = detour.DtClosestHeightPointTriangle(pos[:], vertices[0][:], vertices[1][:], vertices[2][:], &height)
		}
		if !normalFound {
			a := Vec3{X: float64(vertices[0][0]), Y: float64(vertices[0][1]), Z: float64(vertices[0][2])}
			b := Vec3{X: float64(vertices[1][0]), Y: float64(vertices[1][1]), Z: float64(vertices[1][2])}
			c := Vec3{X: float64(vertices[2][0]), Y: float64(vertices[2][1]), Z: float64(vertices[2][2])}
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
			normal = Vec3{X: n.X / length, Y: n.Y / length, Z: n.Z / length}
			normalFound = true
		}
		if heightFound && normalFound {
			break
		}
	}
	if heightFound {
		return height, normal, detour.DT_SUCCESS
	}
	// Keep the existing selected-polygon edge fallback and its bounded numeric
	// margin unchanged. It is intentionally not a nearest-floor lookup.
	status := q.query.GetPolyHeight(ref, pos[:], &height)
	return height, normal, status
}
