package navmesh

import (
	"context"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
)

// RepairPath trims a corridor to the actual current surface and regenerates its
// corners. Unchanged tile revisions can be remapped into a new snapshot. A
// changed suffix becomes a partial route; movement outside the corridor requests
// a replan instead of silently teleporting or doing an unbounded search.
// An input corridor exceeding this query's PathPolygons limit is rejected with
// ResourceExhausted before scratch is allocated.
func (q *Query) RepairPath(ctx context.Context, path *Path, current Surface, f Filter) PathResult {
	if st := q.begin(ctx); st.Code != Complete {
		return PathResult{Status: st}
	}
	defer q.end()
	return q.repairPath(ctx, path, current, f)
}

// repairPath requires exclusive query ownership, including when called by rebase.
func (q *Query) repairPath(ctx context.Context, path *Path, current Surface, f Filter) PathResult {
	if path == nil || path.snapshot == nil || !q.valid(current) {
		return PathResult{Status: failure(Stale, "navmesh: stale corridor/location")}
	}
	if !q.snapshot.Topology().compatible(path.snapshot) {
		return PathResult{Status: failure(Stale, "navmesh: incompatible corridor world/profile")}
	}
	filter, err := q.detourFilter(f)
	if err != nil {
		return PathResult{Status: failure(InvalidInput, err.Error())}
	}
	if len(path.refs) > q.limits.PathPolygons {
		return PathResult{Status: failure(ResourceExhausted, "navmesh: corridor exceeds query capacity")}
	}
	refs := resizeScratch(&q.refs, len(path.refs))[:0]
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
		idx := int(path.snapshot.mesh.DecodePolyIdPoly(ref))
		if idx >= len(next.Polys) {
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
		visited := resizeScratch(&q.visited, q.localMovementLimit())
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
		mergedCount := joinVisited + len(refs) - joinPath
		if mergedCount > q.limits.PathPolygons {
			return PathResult{Status: failure(ResourceExhausted, "navmesh: repaired corridor capacity")}
		}
		merged := resizeScratch(&q.merged, mergedCount)[:0]
		merged = append(merged, visited[:joinVisited]...)
		merged = append(merged, refs[joinPath:]...)
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

// Ground funnels are separated by explicit traversal markers. A funnel over the
// whole corridor can skip an off-mesh portal coincident with its starting XZ,
// especially for vertical links. Such links must still reach host policy.
func (q *Query) straightPath(start, end [3]float32, refs []detour.DtPolyRef, points []float32, flags []detour.DtStraightPathFlags, cornerRefs []detour.DtPolyRef) (detour.DtStatus, int) {
	first, count := 0, 0
	for i, ref := range refs {
		if q.snapshot.mesh.GetOffMeshConnectionByRef(ref) == nil {
			continue
		}
		if i == first || i+1 >= len(refs) {
			return detour.DT_FAILURE | detour.DT_INVALID_PARAM, count
		}
		var from, to [3]float32
		if st := q.snapshot.mesh.GetOffMeshConnectionPolyEndPoints(refs[i-1], ref, from[:], to[:]); detour.DtStatusFailed(st) {
			return st, count
		}
		var n int
		st := q.query.FindStraightPath(start[:], from[:], refs[first:i], i-first, points[count*3:], flags[count:], cornerRefs[count:], &n, len(flags)-count, 0)
		count += n
		if detour.DtStatusFailed(st) || n == 0 {
			return st, count
		}
		// If the output ends at the link entrance, retain ground support until
		// there is also room for its landing. A truncated route must not expose
		// an off-mesh surface as its reachable endpoint.
		if flags[count-1]&detour.DT_STRAIGHTPATH_END != 0 {
			cornerRefs[count-1] = refs[i-1]
		}
		if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL|detour.DT_PARTIAL_RESULT) {
			return st, count
		}
		flags[count-1] = detour.DT_STRAIGHTPATH_OFFMESH_CONNECTION
		cornerRefs[count-1] = ref
		copy(points[(count-1)*3:count*3], from[:])
		start, first = to, i+1
	}
	var n int
	st := q.query.FindStraightPath(start[:], end[:], refs[first:], len(refs)-first, points[count*3:], flags[count:], cornerRefs[count:], &n, len(flags)-count, 0)
	return st, count + n
}
