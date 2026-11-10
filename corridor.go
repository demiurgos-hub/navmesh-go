package navmesh

import (
	"context"
	"math"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
)

// RepairPath trims a corridor to the actual current surface and regenerates its
// corners. Unchanged tile revisions can be remapped into a new snapshot. A
// changed suffix becomes a partial route; movement outside the corridor requests
// a replan instead of silently teleporting or doing an unbounded search.
func (q *Query) RepairPath(ctx context.Context, path *Path, current Surface, f Filter) PathResult {
	if st := q.begin(ctx); st.Code != Complete {
		return PathResult{Status: st}
	}
	defer q.end()
	if path == nil || path.snapshot == nil || !q.valid(current) {
		return PathResult{Status: failure(Stale, "navmesh: stale corridor/location")}
	}
	if path.snapshot.bundle.WorldID != q.snapshot.bundle.WorldID || path.snapshot.bundle.Profile.ID != q.snapshot.bundle.Profile.ID || path.snapshot.bundle.SettingsFingerprint != q.snapshot.bundle.SettingsFingerprint {
		return PathResult{Status: failure(Stale, "navmesh: incompatible corridor world/profile")}
	}
	filter, err := f.detour()
	if err != nil {
		return PathResult{Status: failure(InvalidInput, err.Error())}
	}
	refs := make([]detour.DtPolyRef, 0, len(path.refs))
	status := path.status
	for _, ref := range path.refs {
		if err := ctx.Err(); err != nil {
			return PathResult{Status: Status{Code: Cancelled, Reason: Cancelled, Err: err}}
		}
		var tile *detour.DtMeshTile
		var poly *detour.DtPoly
		if detour.DtStatusFailed(path.snapshot.mesh.GetTileAndPolyByRef(ref, &tile, &poly)) {
			break
		}
		key := artifact.TileKey{X: tile.Header.X, Z: tile.Header.Y, Layer: tile.Header.Layer}
		if rev, ok := q.snapshot.tiles[key]; !ok || rev != path.snapshot.tiles[key] {
			status = Status{Code: Partial, Reason: Stale}
			break
		}
		next := q.snapshot.mesh.GetTileAt(key.X, key.Z, key.Layer)
		if next == nil {
			status = Status{Code: Partial, Reason: Unloaded}
			break
		}
		idx := -1
		for i := range tile.Polys {
			if &tile.Polys[i] == poly {
				idx = i
				break
			}
		}
		if idx < 0 || idx >= len(next.Polys) {
			status = Status{Code: Partial, Reason: Stale}
			break
		}
		mapped := q.snapshot.mesh.GetPolyRefBase(next) + detour.DtPolyRef(idx)
		if !filter.PassFilter(mapped, next, &next.Polys[idx]) {
			status = Status{Code: Partial}
			break
		}
		refs = append(refs, mapped)
	}
	first := -1
	for i, ref := range refs {
		if ref == current.ref {
			first = i
			break
		}
	}
	if first < 0 {
		// A local step can cross a neighboring polygon beside the planned
		// corridor (especially on a seam). Search only the short return route
		// to its current start, then splice at a common polygon.
		if len(refs) == 0 {
			return PathResult{Status: failure(Stale, "navmesh: corridor disappeared")}
		}
		a, b := q.local(current.position), q.local(path.start.position)
		visited := make([]detour.DtPolyRef, q.limits.PathPolygons)
		var count int
		var point [3]float32
		var hit bool
		st := q.query.MoveAlongSurface(current.ref, a[:], b[:], filter, point[:], visited, &count, len(visited), &hit)
		if detour.DtStatusFailed(st) || detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL) {
			return PathResult{Status: failure(Stale, "navmesh: local corridor repair failed")}
		}
		joinPath, joinVisited := -1, -1
		for i := len(refs) - 1; i >= 0 && joinPath < 0; i-- {
			for j := 0; j < count; j++ {
				if refs[i] == visited[j] {
					joinPath = i
					joinVisited = j
					break
				}
			}
		}
		if joinPath < 0 {
			var tile *detour.DtMeshTile
			var poly *detour.DtPoly
			q.snapshot.mesh.GetTileAndPolyByRef(current.ref, &tile, &poly)
			for link := poly.FirstLink; link != detour.DT_NULL_LINK && joinPath < 0; link = tile.Links[link].Next {
				for i, ref := range refs {
					if tile.Links[link].Ref == ref {
						joinPath = i
						joinVisited = 1
						visited[0] = current.ref
						break
					}
				}
			}
			if joinPath < 0 {
				return PathResult{Status: failure(Stale, "navmesh: actual movement left the corridor; replan required")}
			}
		}
		merged := append([]detour.DtPolyRef(nil), visited[:joinVisited]...)
		merged = append(merged, refs[joinPath:]...)
		if len(merged) > q.limits.PathPolygons {
			return PathResult{Status: failure(ResourceExhausted, "navmesh: repaired corridor capacity")}
		}
		refs = merged
		first = 0
	}
	refs = refs[first:]
	end := q.local(path.end.position)
	if status.Code == Partial || len(refs)+first != len(path.refs) {
		if detour.DtStatusFailed(q.query.ClosestPointOnPoly(refs[len(refs)-1], end[:], end[:], nil)) {
			return PathResult{Status: failure(NoSurface, "navmesh: corridor endpoint disappeared")}
		}
	}
	return q.pathFromRefs(current, refs, end, status)
}

func (q *Query) normal(ref detour.DtPolyRef, pos [3]float32) Vec3 {
	var tile *detour.DtMeshTile
	var poly *detour.DtPoly
	if detour.DtStatusFailed(q.snapshot.mesh.GetTileAndPolyByRef(ref, &tile, &poly)) || poly.GetType() != detour.DT_POLYTYPE_GROUND {
		return Vec3{}
	}
	index := -1
	for i := range tile.Polys {
		if &tile.Polys[i] == poly {
			index = i
			break
		}
	}
	if index < 0 || index >= len(tile.DetailMeshes) {
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
