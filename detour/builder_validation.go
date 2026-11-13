package detour

import "fmt"

// ValidateCreateParams checks counts, slice lengths, finite settings and all
// polygon, detail-mesh and traversal-link indices without building a tile.
func ValidateCreateParams(p *DtNavMeshCreateParams) error {
	return validateCreateParams(p)
}

func validCreateParams(p *DtNavMeshCreateParams) bool {
	return validateCreateParams(p) == nil
}

func validateCreateParams(p *DtNavMeshCreateParams) error {
	if p == nil || p.Nvp < 3 || p.Nvp > DT_VERTS_PER_POLYGON || p.VertCount < 3 || p.VertCount >= 65535 || p.PolyCount <= 0 || p.PolyCount > 65535 || p.OffMeshConCount < 0 || int64(p.PolyCount)+int64(p.OffMeshConCount) > 65535 || int64(p.VertCount)+2*int64(p.OffMeshConCount) > 65535 || p.TileLayer < 0 {
		return fmt.Errorf("detour: invalid tile counts, polygon width or layer")
	}
	if int64(len(p.Verts)) < int64(p.VertCount)*3 || int64(len(p.Polys)) < int64(p.PolyCount)*int64(p.Nvp)*2 || int64(len(p.PolyFlags)) < int64(p.PolyCount) || int64(len(p.PolyAreas)) < int64(p.PolyCount) {
		return fmt.Errorf("detour: truncated polygon or vertex arrays")
	}
	if !finite(p.Cs) || !finite(p.Ch) || p.Cs <= 0 || p.Ch <= 0 || !finite3(p.Bmin[:]) || !finite3(p.Bmax[:]) {
		return fmt.Errorf("detour: invalid cell dimensions or bounds")
	}
	for i := range p.Bmin {
		if p.Bmin[i] > p.Bmax[i] {
			return fmt.Errorf("detour: inverted bounds on axis %d", i)
		}
	}
	for _, v := range []float32{p.WalkableHeight, p.WalkableRadius, p.WalkableClimb} {
		if !finite(v) || v < 0 {
			return fmt.Errorf("detour: invalid walkable dimension")
		}
	}
	if p.DetailVertsCount < 0 || p.DetailTriCount < 0 || int64(len(p.DetailVerts)) < int64(p.DetailVertsCount)*3 || int64(len(p.DetailTris)) < int64(p.DetailTriCount)*4 {
		return fmt.Errorf("detour: invalid detail counts or truncated arrays")
	}
	if p.DetailMeshes != nil && int64(len(p.DetailMeshes)) < int64(p.PolyCount)*4 {
		return fmt.Errorf("detour: truncated detail mesh array")
	}
	if p.DetailMeshes == nil && (p.DetailVertsCount != 0 || p.DetailTriCount != 0) {
		return fmt.Errorf("detour: detail data without detail meshes")
	}
	for _, v := range p.DetailVerts {
		if !finite(v) {
			return fmt.Errorf("detour: nonfinite detail vertex")
		}
	}
	for i := 0; i < int(p.PolyCount); i++ {
		if p.PolyAreas[i] >= uint8(DT_MAX_AREAS) {
			return fmt.Errorf("detour: polygon %d has invalid area %d", i, p.PolyAreas[i])
		}
		poly := p.Polys[i*int(p.Nvp)*2:]
		nv := 0
		for j := 0; j < int(p.Nvp); j++ {
			if poly[j] == MESH_NULL_IDX {
				break
			}
			if int32(poly[j]) >= p.VertCount {
				return fmt.Errorf("detour: polygon %d vertex %d index exceeds vertex count", i, j)
			}
			nv++
			for k := 0; k < j; k++ {
				if poly[k] == poly[j] {
					return fmt.Errorf("detour: polygon %d repeats vertex %d at %d and %d", i, poly[j], k, j)
				}
			}
			n := poly[int(p.Nvp)+j]
			if n&0x8000 != 0 {
				if n != MESH_NULL_IDX && (n&^uint16(0x800f) != 0 || n&0xf > 3) {
					return fmt.Errorf("detour: polygon %d edge %d has invalid portal %x", i, j, n)
				}
			} else if int32(n) >= p.PolyCount {
				return fmt.Errorf("detour: polygon %d edge %d neighbor exceeds polygon count", i, j)
			}
		}
		if nv < 3 {
			return fmt.Errorf("detour: polygon %d has fewer than three vertices", i)
		}
		if p.DetailMeshes != nil {
			d := p.DetailMeshes[i*4:]
			vb, vc, tb, tc := uint64(d[0]), uint64(d[1]), uint64(d[2]), uint64(d[3])
			if vc < uint64(nv) || vc > 255 || tc == 0 || tc > 255 || vb+vc > uint64(p.DetailVertsCount) || tb+tc > uint64(p.DetailTriCount) {
				return fmt.Errorf("detour: polygon %d has invalid detail range: verts %d+%d/%d, tris %d+%d/%d, polygon verts %d", i, vb, vc, p.DetailVertsCount, tb, tc, p.DetailTriCount, nv)
			}
			for k := tb; k < tb+tc; k++ {
				for j := 0; j < 3; j++ {
					if uint64(p.DetailTris[int(k)*4+j]) >= vc {
						return fmt.Errorf("detour: polygon %d detail triangle %d vertex %d exceeds detail vertex count %d", i, k, j, vc)
					}
				}
			}
		}
	}
	n := int(p.OffMeshConCount)
	if len(p.OffMeshConVerts) < n*6 || len(p.OffMeshConRad) < n || len(p.OffMeshConFlags) < n || len(p.OffMeshConAreas) < n || len(p.OffMeshConDir) < n || p.OffMeshConUserID != nil && len(p.OffMeshConUserID) < n {
		return fmt.Errorf("detour: truncated off-mesh connection arrays")
	}
	for i := 0; i < n; i++ {
		if !finite3(p.OffMeshConVerts[i*6:]) || !finite3(p.OffMeshConVerts[i*6+3:]) || !finite(p.OffMeshConRad[i]) || p.OffMeshConRad[i] < 0 || p.OffMeshConAreas[i] >= uint8(DT_MAX_AREAS) || p.OffMeshConDir[i] > DT_OFFMESH_CON_BIDIR {
			return fmt.Errorf("detour: invalid off-mesh connection %d", i)
		}
	}
	return validateGroundGraph(int(p.PolyCount), func(i int) groundPolygon {
		var out groundPolygon
		poly := p.Polys[i*int(p.Nvp)*2:]
		for j := 0; j < int(p.Nvp) && poly[j] != MESH_NULL_IDX; j++ {
			v := int(poly[j]) * 3
			out.vertices[j] = [3]float64{float64(p.Verts[v]), float64(p.Verts[v+1]), float64(p.Verts[v+2])}
			out.neighbors[j] = -1
			if n := poly[int(p.Nvp)+j]; n&0x8000 == 0 {
				out.neighbors[j] = int(n)
			}
			out.count++
		}
		return out
	})
}

// Ground polygons must be convex clockwise surfaces in XZ. Internal links
// must connect the same reversed geometric edge in both directions. Comparing
// coordinates permits duplicated shared vertices in externally authored tiles.
type groundPolygon struct {
	vertices  [DT_VERTS_PER_POLYGON][3]float64
	neighbors [DT_VERTS_PER_POLYGON]int
	count     int
}

func validGroundGraph(count int, polygon func(int) groundPolygon) bool {
	return validateGroundGraph(count, polygon) == nil
}

func validateGroundGraph(count int, polygon func(int) groundPolygon) error {
	for i := 0; i < count; i++ {
		p := polygon(i)
		area := 0.0
		for j := 0; j < p.count; j++ {
			a, b := p.vertices[j], p.vertices[(j+1)%p.count]
			for k := 0; k < j; k++ {
				if a[0] == p.vertices[k][0] && a[2] == p.vertices[k][2] {
					return fmt.Errorf("detour: polygon %d repeats XZ position at vertices %d and %d", i, k, j)
				}
			}
			if a[0] == b[0] && a[2] == b[2] {
				return fmt.Errorf("detour: polygon %d edge %d has zero XZ length", i, j)
			}
			// Translate to the first vertex to avoid cancellation at large
			// offsets; float64 arithmetic contains all float32 input products.
			origin := p.vertices[0]
			area += (a[0]-origin[0])*(b[2]-origin[2]) - (b[0]-origin[0])*(a[2]-origin[2])
			for k := 0; k < p.count; k++ {
				c := p.vertices[k]
				if (b[0]-a[0])*(c[2]-a[2])-(b[2]-a[2])*(c[0]-a[0]) > 0 {
					return fmt.Errorf("detour: polygon %d is not convex clockwise at edge %d, vertex %d", i, j, k)
				}
			}
			n := p.neighbors[j]
			if n < 0 {
				continue
			}
			if n >= count || n == i {
				return fmt.Errorf("detour: polygon %d edge %d has invalid neighbor %d", i, j, n)
			}
			neighbor := polygon(n)
			matched := false
			for k := 0; k < neighbor.count; k++ {
				if neighbor.neighbors[k] == i && neighbor.vertices[k] == b && neighbor.vertices[(k+1)%neighbor.count] == a {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("detour: polygon %d edge %d has no matching reverse edge in neighbor %d", i, j, n)
			}
		}
		if area >= 0 {
			return fmt.Errorf("detour: polygon %d has nonnegative signed XZ area %g", i, area)
		}
	}
	return nil
}
