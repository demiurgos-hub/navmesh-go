package navmesh

import (
	"context"
	"math"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

// RebaseResult transfers unchanged support and an optional path to a new query.
type RebaseResult struct {
	Status
	Surface Surface
	Path    *Path
}

// RebasePath maps the exact unchanged support polygon into this query's
// snapshot, checks actual body support, and repairs the optional corridor.
// It never projects onto another floor. Retain the previous snapshot throughout
// the call. A nil path transfers only support; a changed suffix remains partial.
func (q *Query) RebasePath(ctx context.Context, path *Path, previous Surface, actual Vec3, tolerance float64, f Filter) RebaseResult {
	if st := q.begin(ctx); st.Code != Complete {
		return RebaseResult{Status: st}
	}
	defer q.end()
	current, _, status := q.rebaseSupport(previous, actual, tolerance, f)
	if status.Code != Complete {
		return RebaseResult{Status: status}
	}
	if path == nil {
		return RebaseResult{Surface: current}
	}
	result := q.repairPath(ctx, path, current, f)
	return RebaseResult{Status: result.Status, Surface: current, Path: result.Path}
}

// RebaseUnchangedPath transfers exact unchanged support and an optional entire
// corridor without repairing it or changing its corners, endpoint, or status.
// Every corridor polygon must remain present and filter-eligible, and current
// support must belong to that corridor. A changed suffix is rejected as Stale.
// PathPolygons and Corners limits apply to the retained corridor and corners.
// Retain the previous snapshots throughout the call. The result retains only
// this query's snapshot; private immutable path arrays may be shared safely.
// Callers preserving steering progress must also preserve their corner cursor
// and any outstanding need to repair after a lateral deviation. A nil path
// transfers only support. This operation never chooses another support floor.
func (q *Query) RebaseUnchangedPath(ctx context.Context, path *Path, previous Surface, actual Vec3, tolerance float64, f Filter) RebaseResult {
	if st := q.begin(ctx); st.Code != Complete {
		return RebaseResult{Status: st}
	}
	defer q.end()
	current, filter, status := q.rebaseSupport(previous, actual, tolerance, f)
	if status.Code != Complete {
		return RebaseResult{Status: status}
	}
	if path == nil {
		return RebaseResult{Surface: current}
	}
	if len(path.refs) > q.limits.PathPolygons || len(path.corners) > q.limits.Corners {
		return RebaseResult{Status: failure(ResourceExhausted, "navmesh: retained corridor exceeds query capacity")}
	}
	if len(path.refs) == 0 || !q.snapshot.Topology().PathUnchanged(path) || path.start.snapshot != path.snapshot || path.end.snapshot != path.snapshot || !path.start.position.Finite() || !path.end.position.Finite() {
		return RebaseResult{Status: failure(Stale, "navmesh: corridor changed or disappeared")}
	}
	refs := path.refs
	ownedRefs := false
	var start, end detour.DtPolyRef
	containsSupport := false
	for i, oldRef := range path.refs {
		if err := ctx.Err(); err != nil {
			return RebaseResult{Status: Status{Code: Cancelled, Reason: Cancelled, Err: err}}
		}
		var oldTile *detour.DtMeshTile
		var oldPoly *detour.DtPoly
		if detour.DtStatusFailed(path.snapshot.mesh.GetTileAndPolyByRef(oldRef, &oldTile, &oldPoly)) {
			return RebaseResult{Status: failure(Stale, "navmesh: invalid retained corridor polygon")}
		}
		tile := q.snapshot.mesh.GetTileAt(oldTile.Header.X, oldTile.Header.Y, oldTile.Header.Layer)
		index := int(path.snapshot.mesh.DecodePolyIdPoly(oldRef))
		if tile == nil || index >= len(tile.Polys) || tile.Polys[index].GetType() != oldPoly.GetType() {
			return RebaseResult{Status: failure(Stale, "navmesh: corridor identity unavailable")}
		}
		ref := q.snapshot.mesh.GetPolyRefBase(tile) + detour.DtPolyRef(index)
		if !filter.PassFilter(ref, tile, &tile.Polys[index]) {
			return RebaseResult{Status: failure(Stale, "navmesh: corridor excluded by filter")}
		}
		if ref != oldRef && !ownedRefs {
			refs = append([]detour.DtPolyRef(nil), path.refs...)
			ownedRefs = true
		}
		if ownedRefs {
			refs[i] = ref
		}
		if oldRef == path.start.ref {
			start = ref
		}
		if oldRef == path.end.ref {
			end = ref
		}
		containsSupport = containsSupport || ref == current.ref
	}
	if !containsSupport || start == 0 || end == 0 {
		return RebaseResult{Status: failure(Stale, "navmesh: support or endpoint is outside retained corridor")}
	}
	transferred := &Path{status: path.status, snapshot: q.snapshot, refs: refs, corners: path.corners, tiles: path.tiles,
		start: Surface{snapshot: q.snapshot, ref: start, position: path.start.position},
		end:   Surface{snapshot: q.snapshot, ref: end, position: path.end.position}}
	return RebaseResult{Status: path.status, Surface: current, Path: transferred}
}

// rebaseSupport requires exclusive query ownership. It maps only the retained
// polygon and checks the actual body against it, never a nearest-floor search.
func (q *Query) rebaseSupport(previous Surface, actual Vec3, tolerance float64, f Filter) (Surface, *detour.DtQueryFilter, Status) {
	if !actual.Finite() || !q.representable(actual) || math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 {
		return Surface{}, nil, failure(InvalidInput, "navmesh: invalid support position/tolerance")
	}
	if !q.snapshot.Topology().SurfaceUnchanged(previous) {
		return Surface{}, nil, failure(Stale, "navmesh: support tile changed or disappeared")
	}
	filter, err := q.detourFilter(f)
	if err != nil {
		return Surface{}, nil, failure(InvalidInput, err.Error())
	}
	var oldTile *detour.DtMeshTile
	var oldPoly *detour.DtPoly
	if detour.DtStatusFailed(previous.snapshot.mesh.GetTileAndPolyByRef(previous.ref, &oldTile, &oldPoly)) || oldPoly.GetType() != detour.DT_POLYTYPE_GROUND {
		return Surface{}, nil, failure(Stale, "navmesh: invalid support polygon")
	}
	tile := q.snapshot.mesh.GetTileAt(oldTile.Header.X, oldTile.Header.Y, oldTile.Header.Layer)
	index := int(previous.snapshot.mesh.DecodePolyIdPoly(previous.ref))
	if tile == nil || index >= len(tile.Polys) {
		return Surface{}, nil, failure(Stale, "navmesh: support identity unavailable")
	}
	ref := q.snapshot.mesh.GetPolyRefBase(tile) + detour.DtPolyRef(index)
	if !filter.PassFilter(ref, tile, &tile.Polys[index]) {
		return Surface{}, nil, failure(NoSurface, "navmesh: support excluded by filter")
	}
	point := q.local(actual)
	var closest [3]float32
	if detour.DtStatusFailed(q.query.ClosestPointOnPoly(ref, point[:], closest[:], nil)) {
		return Surface{}, nil, failure(NoSurface, "navmesh: support projection failed")
	}
	position := q.world(closest)
	dx, dy, dz := position.X-actual.X, position.Y-actual.Y, position.Z-actual.Z
	if math.Hypot(math.Hypot(dx, dy), dz) > tolerance {
		return Surface{}, nil, failure(NoSurface, "navmesh: body left retained support")
	}
	current := Surface{snapshot: q.snapshot, ref: ref, position: position}
	return current, filter, Status{}
}
