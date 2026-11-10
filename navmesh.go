// Package navmesh provides checked queries over immutable, pure-Go navigation
// snapshots. Query objects are exclusively owned; snapshots may be shared.
package navmesh

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// Vec3 is a Y-up position in meters.
type Vec3 = geom.Vec3

// Code distinguishes semantic query outcomes and limiting conditions.
type Code uint8

const (
	Complete Code = iota
	Partial
	Unreachable
	NoSurface
	InvalidInput
	Unloaded
	Cancelled
	ResourceExhausted
	Stale
	Ambiguous
	Failed
	NotReady
)

// Status keeps the result separate from a limiting cause. Partial routes may
// carry ResourceExhausted or Unloaded as their Reason.
type Status struct {
	Code   Code
	Reason Code
	Err    error
}

func failure(c Code, msg string) Status { return Status{Code: c, Reason: c, Err: errors.New(msg)} }

// Surface is an immutable location qualified by its originating snapshot.
// Its zero value is invalid. It must never be persisted as a world identity.
type Surface struct {
	snapshot *Snapshot
	ref      detour.DtPolyRef
	position Vec3
}

// Position returns the projected world position.
func (s Surface) Position() Vec3 { return s.position }

// Revision returns the originating snapshot revision.
func (s Surface) Revision() uint64 {
	if s.snapshot == nil {
		return 0
	}
	return s.snapshot.Revision()
}

// Valid reports whether this is a nonzero surface handle.
func (s Surface) Valid() bool { return s.snapshot != nil && s.ref != 0 }

// Filter is copied into query-owned Detour state. Zero IncludeFlags means all.
type Filter struct {
	IncludeFlags, ExcludeFlags uint16
	AreaCosts                  [64]float32
}

// Limits bounds a query's node pool and result arrays.
type Limits struct{ Nodes, PathPolygons, Corners, Candidates int }

// DefaultLimits returns bounded query defaults.
func DefaultLimits() Limits { return Limits{4096, 2048, 512, 4096} }

// ProjectionOptions specifies bounded snapping. Hint preserves floor identity
// if it is valid and inside all bounds. Tied distinct surfaces are ambiguous.
type ProjectionOptions struct {
	Horizontal, Up, Down float64
	Hint                 Surface
}

// ProjectionResult contains an optional projected surface.
type ProjectionResult struct {
	Status
	Surface Surface
}

// TileRevision is a route dependency.
type TileRevision struct {
	Key      artifact.TileKey
	Revision uint64
}

// Traversal describes the directed use of an authored link.
type Traversal struct {
	ID       string
	From, To Vec3
	Reverse  bool
}

// Corner is a straight-path target; Link requires host-controlled traversal.
type Corner struct {
	Position Vec3
	Link     *Traversal
}

// Path is an immutable polygon corridor with copied public views.
type Path struct {
	status     Status
	snapshot   *Snapshot
	refs       []detour.DtPolyRef
	corners    []Corner
	tiles      []TileRevision
	start, end Surface
}

// Corners returns a deep copy of steering targets.
func (p *Path) Corners() []Corner {
	if p == nil {
		return nil
	}
	out := append([]Corner(nil), p.corners...)
	for i := range out {
		if out[i].Link != nil {
			v := *out[i].Link
			out[i].Link = &v
		}
	}
	return out
}

// Tiles returns a copy of dependency revisions.
func (p *Path) Tiles() []TileRevision {
	if p == nil {
		return nil
	}
	return append([]TileRevision(nil), p.tiles...)
}

// End returns the reachable endpoint, which may differ from the requested goal.
func (p *Path) End() Surface {
	if p == nil {
		return Surface{}
	}
	return p.end
}

// PathResult returns a complete or useful start-side partial path.
type PathResult struct {
	Status
	Path *Path
}

// MoveResult returns the constrained position and visited surface references.
type MoveResult struct {
	Status
	Surface Surface
	Visited []Surface
	Normal  Vec3
}

// HeightResult returns a surface-qualified world height.
type HeightResult struct {
	Status
	Height float64
}

// RaycastResult reports a walkability ray, not general three-dimensional collision.
type RaycastResult struct {
	Status
	Hit              bool
	Position, Normal Vec3
	Fraction         float64
}

// Snapshot owns an immutable runtime mesh and all its neighboring links.
type Snapshot struct {
	mesh       *detour.DtNavMesh
	bundle     artifact.Bundle
	tiles      map[artifact.TileKey]uint64
	links      map[uint32]geom.Link
	incomplete bool
}

// NewSnapshot validates and copies a bundle, assembling every tile privately.
// Failure cannot mutate another snapshot or caller-owned data.
func NewSnapshot(b artifact.Bundle) (*Snapshot, error) {
	b, err := b.Clone()
	if err != nil {
		return nil, err
	}
	if b.Revision == 0 {
		b.Revision = 1
	}
	s := &Snapshot{bundle: b, mesh: detour.DtAllocNavMesh(), tiles: map[artifact.TileKey]uint64{}, links: map[uint32]geom.Link{}}
	maxPolys := uint32(1)
	for _, t := range b.Tiles {
		n := uint32(t.Parameters.PolyCount + t.Parameters.OffMeshConCount)
		if n > maxPolys {
			maxPolys = n
		}
		s.tiles[t.Key] = t.Revision
	}
	n := len(b.Tiles)
	if n == 0 {
		n = 1
	}
	width := float32(b.Settings.CellSize * float64(b.Settings.TileSize))
	status := s.mesh.Init(&detour.DtNavMeshParams{TileWidth: width, TileHeight: width, MaxTiles: uint32(n), MaxPolys: maxPolys})
	if detour.DtStatusFailed(status) {
		return nil, fmt.Errorf("navmesh: tile/reference capacity exceeded (%#x)", status)
	}
	for _, t := range b.Tiles {
		if t.Empty {
			continue
		}
		p := t.Parameters
		var data []byte
		var size int
		if !detour.DtCreateNavMeshData(&p, &data, &size) {
			return nil, fmt.Errorf("navmesh: invalid construction data for %v", t.Key)
		}
		if err := detour.ValidateNavMeshData(data); err != nil {
			return nil, err
		}
		if st := s.mesh.AddTile(data, size, detour.DT_TILE_FREE_DATA, 0, nil); detour.DtStatusFailed(st) {
			return nil, fmt.Errorf("navmesh: add tile %v: %#x", t.Key, st)
		}
	}
	for _, l := range b.Links {
		s.links[geom.LinkKey(l.ID)] = l
	}
	tw := b.Settings.CellSize * float64(b.Settings.TileSize)
	lo, hi := b.Settings.Bounds.Min, b.Settings.Bounds.Max
	ox, oz := b.Settings.Origin.X, b.Settings.Origin.Z
	x0, x1 := int64(math.Floor((lo.X-ox)/tw)), int64(math.Ceil((hi.X-ox)/tw))
	z0, z1 := int64(math.Floor((lo.Z-oz)/tw)), int64(math.Ceil((hi.Z-oz)/tw))
	if x1-x0 > 1<<20 || z1-z0 > 1<<20 || (x1-x0)*(z1-z0) > int64(len(b.Tiles)) {
		s.incomplete = true
	} else {
		for z := z0; z < z1; z++ {
			for x := x0; x < x1; x++ {
				if _, ok := s.tiles[artifact.TileKey{X: int32(x), Z: int32(z)}]; !ok {
					s.incomplete = true
				}
			}
		}
	}
	return s, nil
}

// Revision returns the committed revision of this snapshot.
func (s *Snapshot) Revision() uint64 {
	if s == nil {
		return 0
	}
	return s.bundle.Revision
}

// Profile returns the baked physical profile by value.
func (s *Snapshot) Profile() geom.Profile { return s.bundle.Profile }

// Bundle returns an independent copy suitable for persistence or a new build.
func (s *Snapshot) Bundle() (artifact.Bundle, error) { return s.bundle.Clone() }

// NewQuery allocates exclusively owned scratch for an immutable snapshot.
func (s *Snapshot) NewQuery(l Limits) (*Query, error) {
	if s == nil {
		return nil, errors.New("navmesh: nil snapshot")
	}
	d := DefaultLimits()
	if l.Nodes == 0 {
		l.Nodes = d.Nodes
	}
	if l.PathPolygons == 0 {
		l.PathPolygons = d.PathPolygons
	}
	if l.Corners == 0 {
		l.Corners = d.Corners
	}
	if l.Candidates == 0 {
		l.Candidates = d.Candidates
	}
	if l.Nodes < 4 || l.Nodes > 65535 || l.PathPolygons < 1 || l.PathPolygons > 65535 || l.Corners < 2 || l.Corners > 65535 || l.Candidates < 1 || l.Candidates > 65535 {
		return nil, errors.New("navmesh: invalid query limits")
	}
	q := &Query{snapshot: s, limits: l, query: detour.DtAllocNavMeshQuery()}
	if st := q.query.Init(s.mesh, l.Nodes); detour.DtStatusFailed(st) {
		return nil, fmt.Errorf("navmesh: query init %#x", st)
	}
	return q, nil
}

// Query owns nonconcurrent scratch. Concurrent/reentrant use returns an error.
type Query struct {
	snapshot *Snapshot
	query    *detour.DtNavMeshQuery
	limits   Limits
	busy     uint32
}

func (q *Query) begin(ctx context.Context) Status {
	if q == nil || q.snapshot == nil || q.query == nil || ctx == nil {
		return failure(InvalidInput, "navmesh: nil query/context")
	}
	if err := ctx.Err(); err != nil {
		return Status{Code: Cancelled, Reason: Cancelled, Err: err}
	}
	if !atomic.CompareAndSwapUint32(&q.busy, 0, 1) {
		return failure(InvalidInput, "navmesh: query already in use")
	}
	return Status{}
}
func (q *Query) end() { atomic.StoreUint32(&q.busy, 0) }
func (q *Query) valid(s Surface) bool {
	return s.snapshot == q.snapshot && s.ref != 0 && q.snapshot.mesh.IsValidPolyRef(s.ref) && s.position.Finite()
}
func (q *Query) local(v Vec3) [3]float32 {
	o := q.snapshot.bundle.Settings.Origin
	return [3]float32{float32(v.X - o.X), float32(v.Y - o.Y), float32(v.Z - o.Z)}
}

// representable rejects float64 values that overflow the local runtime space.
func (q *Query) representable(v Vec3) bool {
	for _, f := range q.local(v) {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return false
		}
	}
	return true
}

func (q *Query) passes(ref detour.DtPolyRef, filter *detour.DtQueryFilter) bool {
	var tile *detour.DtMeshTile
	var poly *detour.DtPoly
	return !detour.DtStatusFailed(q.snapshot.mesh.GetTileAndPolyByRef(ref, &tile, &poly)) && filter.PassFilter(ref, tile, poly)
}
func (q *Query) world(v [3]float32) Vec3 {
	o := q.snapshot.bundle.Settings.Origin
	return Vec3{X: float64(v[0]) + o.X, Y: float64(v[1]) + o.Y, Z: float64(v[2]) + o.Z}
}
func (f Filter) detour() (*detour.DtQueryFilter, error) {
	d := detour.DtAllocDtQueryFilter()
	include := f.IncludeFlags
	if include == 0 {
		include = 65535
	}
	d.SetIncludeFlags(include)
	d.SetExcludeFlags(f.ExcludeFlags)
	for i, c := range f.AreaCosts {
		if c == 0 {
			c = 1
		}
		if c < 1 || math.IsInf(float64(c), 0) || math.IsNaN(float64(c)) {
			return nil, errors.New("navmesh: costs must be finite and >=1")
		}
		d.SetAreaCost(i, c)
	}
	return d, nil
}
func (q *Query) loaded(p Vec3) bool {
	s := q.snapshot.bundle.Settings
	tw := s.CellSize * float64(s.TileSize)
	x, z := math.Floor((p.X-s.Origin.X)/tw), math.Floor((p.Z-s.Origin.Z)/tw)
	if x < math.MinInt32 || x > math.MaxInt32 || z < math.MinInt32 || z > math.MaxInt32 {
		return false
	}
	_, ok := q.snapshot.tiles[artifact.TileKey{X: int32(x), Z: int32(z)}]
	return ok
}

// Project selects a detail surface within explicit horizontal/up/down bounds.
func (q *Query) Project(ctx context.Context, p Vec3, o ProjectionOptions, f Filter) ProjectionResult {
	if st := q.begin(ctx); st.Code != Complete {
		return ProjectionResult{Status: st}
	}
	defer q.end()
	if !p.Finite() || !q.representable(p) || !finiteNonnegative(o.Horizontal) || !finiteNonnegative(o.Up) || !finiteNonnegative(o.Down) {
		return ProjectionResult{Status: failure(InvalidInput, "navmesh: invalid projection")}
	}
	filter, err := f.detour()
	if err != nil {
		return ProjectionResult{Status: failure(InvalidInput, err.Error())}
	}
	pos := q.local(p)
	within := func(v [3]float32) bool {
		w := q.world(v)
		dx, dz := w.X-p.X, w.Z-p.Z
		return dx*dx+dz*dz <= o.Horizontal*o.Horizontal+1e-12 && w.Y >= p.Y-o.Down-1e-6 && w.Y <= p.Y+o.Up+1e-6
	}
	if o.Hint.Valid() {
		if !q.valid(o.Hint) {
			return ProjectionResult{Status: failure(Stale, "navmesh: stale floor hint")}
		}
		var v [3]float32
		var tile *detour.DtMeshTile
		var poly *detour.DtPoly
		q.snapshot.mesh.GetTileAndPolyByRef(o.Hint.ref, &tile, &poly)
		if filter.PassFilter(o.Hint.ref, tile, poly) && !detour.DtStatusFailed(q.query.ClosestPointOnPoly(o.Hint.ref, pos[:], v[:], nil)) && within(v) {
			return ProjectionResult{Surface: Surface{q.snapshot, o.Hint.ref, q.world(v)}}
		}
	}
	// Clip the search to the fixed dataset bounds before converting to float32.
	// This also bounds Detour's tile enumeration for hostile/accidental radii.
	bounds := q.snapshot.bundle.Settings.Bounds
	lo := Vec3{X: math.Max(bounds.Min.X, p.X-o.Horizontal), Y: math.Max(bounds.Min.Y, p.Y-o.Down), Z: math.Max(bounds.Min.Z, p.Z-o.Horizontal)}
	hi := Vec3{X: math.Min(bounds.Max.X, p.X+o.Horizontal), Y: math.Min(bounds.Max.Y, p.Y+o.Up), Z: math.Min(bounds.Max.Z, p.Z+o.Horizontal)}
	if lo.X > hi.X || lo.Y > hi.Y || lo.Z > hi.Z {
		code := NoSurface
		if !q.loaded(p) {
			code = Unloaded
		}
		return ProjectionResult{Status: failure(code, "navmesh: projection outside dataset")}
	}
	lower, upper := q.local(lo), q.local(hi)
	var center, ext [3]float32
	for i := range center {
		center[i] = lower[i] + (upper[i]-lower[i])*.5
		ext[i] = (upper[i] - lower[i]) * .5
	}
	refs := make([]detour.DtPolyRef, q.limits.Candidates)
	var n int
	st := q.query.QueryPolygons(center[:], ext[:], filter, refs, &n, len(refs))
	if detour.DtStatusFailed(st) {
		return ProjectionResult{Status: failure(InvalidInput, "navmesh: invalid projection bounds")}
	}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL) {
		return ProjectionResult{Status: failure(ResourceExhausted, "navmesh: projection candidate limit")}
	}
	best := math.Inf(1)
	tieTolerance := 1e-8
	for _, v := range pos {
		ulp := math.Abs(float64(math.Nextafter32(v, float32(math.Inf(1)))) - float64(v))
		tieTolerance = math.Max(tieTolerance, 8*ulp*math.Max(1, float64(ext[0]+ext[1]+ext[2])))
	}
	var chosen Surface
	ambiguous := false
	for _, ref := range refs[:n] {
		if err := ctx.Err(); err != nil {
			return ProjectionResult{Status: Status{Code: Cancelled, Reason: Cancelled, Err: err}}
		}
		var v [3]float32
		if detour.DtStatusFailed(q.query.ClosestPointOnPoly(ref, pos[:], v[:], nil)) || !within(v) {
			continue
		}
		w := q.world(v)
		dist := (w.X-p.X)*(w.X-p.X) + (w.Y-p.Y)*(w.Y-p.Y) + (w.Z-p.Z)*(w.Z-p.Z)
		if dist < best-tieTolerance {
			chosen = Surface{q.snapshot, ref, w}
			best = dist
			ambiguous = false
		} else if math.Abs(dist-best) <= tieTolerance && math.Abs(w.Y-chosen.position.Y) > 1e-5 {
			ambiguous = true
		}
	}
	if ambiguous {
		return ProjectionResult{Status: failure(Ambiguous, "navmesh: equally near stacked surfaces; supply a floor hint")}
	}
	if !chosen.Valid() {
		code := NoSurface
		if !q.loaded(p) {
			code = Unloaded
		}
		return ProjectionResult{Status: failure(code, "navmesh: no surface within projection bounds")}
	}
	return ProjectionResult{Surface: chosen}
}
func finiteNonnegative(v float64) bool { return v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }

// FindPath runs a sliced search with cancellation between at most 512 expansions.
func (q *Query) FindPath(ctx context.Context, start, end Surface, f Filter) PathResult {
	if st := q.begin(ctx); st.Code != Complete {
		return PathResult{Status: st}
	}
	defer q.end()
	if !q.valid(start) || !q.valid(end) {
		return PathResult{Status: failure(Stale, "navmesh: invalid or stale path endpoint")}
	}
	filter, err := f.detour()
	if err != nil {
		return PathResult{Status: failure(InvalidInput, err.Error())}
	}
	a, b := q.local(start.position), q.local(end.position)
	if !q.passes(start.ref, filter) || !q.passes(end.ref, filter) {
		return PathResult{Status: failure(Unreachable, "navmesh: endpoint excluded by filter")}
	}
	st := q.query.InitSlicedFindPath(start.ref, end.ref, a[:], b[:], filter, 0)
	for detour.DtStatusInProgress(st) {
		if err := ctx.Err(); err != nil {
			return PathResult{Status: Status{Code: Cancelled, Reason: Cancelled, Err: err}}
		}
		st = q.query.UpdateSlicedFindPath(512, nil)
	}
	if detour.DtStatusFailed(st) {
		return PathResult{Status: failure(Failed, "navmesh: path search failed")}
	}
	refs := make([]detour.DtPolyRef, q.limits.PathPolygons)
	var n int
	st = q.query.FinalizeSlicedFindPath(refs, &n, len(refs))
	if detour.DtStatusFailed(st) || n == 0 {
		return PathResult{Status: failure(Unreachable, "navmesh: destination unreachable")}
	}
	refs = refs[:n]
	status := Status{}
	limited := detour.DtStatusDetail(st, detour.DT_OUT_OF_NODES|detour.DT_BUFFER_TOO_SMALL)
	if refs[n-1] != end.ref || detour.DtStatusDetail(st, detour.DT_PARTIAL_RESULT) || limited {
		status.Code = Partial
		if limited {
			status.Reason = ResourceExhausted
		} else if q.snapshot.incomplete {
			status.Reason = Unloaded
		} else {
			status.Reason = Unreachable
		}
		if detour.DtStatusFailed(q.query.ClosestPointOnPoly(refs[n-1], b[:], b[:], nil)) {
			return PathResult{Status: failure(Failed, "navmesh: partial endpoint clamp failed")}
		}
	}
	return q.pathFromRefs(start, refs, b, status)
}

func (q *Query) pathFromRefs(start Surface, refs []detour.DtPolyRef, b [3]float32, status Status) PathResult {
	a := q.local(start.position)
	n := len(refs)
	points := make([]float32, q.limits.Corners*3)
	flags := make([]detour.DtStraightPathFlags, q.limits.Corners)
	cornRefs := make([]detour.DtPolyRef, q.limits.Corners)
	var count int
	st := q.query.FindStraightPath(a[:], b[:], refs, n, points, flags, cornRefs, &count, q.limits.Corners, 0)
	if detour.DtStatusFailed(st) {
		return PathResult{Status: failure(Failed, "navmesh: straight path failed")}
	}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL) {
		status.Code = Partial
		status.Reason = ResourceExhausted
	}
	p := &Path{status: status, snapshot: q.snapshot, refs: append([]detour.DtPolyRef(nil), refs...), start: start, end: Surface{q.snapshot, refs[n-1], q.world(b)}}
	seen := map[artifact.TileKey]bool{}
	for _, ref := range refs {
		var tile *detour.DtMeshTile
		var poly *detour.DtPoly
		q.snapshot.mesh.GetTileAndPolyByRef(ref, &tile, &poly)
		k := artifact.TileKey{X: tile.Header.X, Z: tile.Header.Y, Layer: tile.Header.Layer}
		if !seen[k] {
			p.tiles = append(p.tiles, TileRevision{k, q.snapshot.tiles[k]})
			seen[k] = true
		}
	}
	for i := 0; i < count; i++ {
		c := Corner{Position: q.world([3]float32{points[i*3], points[i*3+1], points[i*3+2]})}
		if flags[i]&detour.DT_STRAIGHTPATH_OFFMESH_CONNECTION != 0 {
			ref := cornRefs[i]
			con := q.snapshot.mesh.GetOffMeshConnectionByRef(ref)
			if con != nil {
				l := q.snapshot.links[con.UserId]
				var prev detour.DtPolyRef
				for j, r := range refs {
					if r == ref && j > 0 {
						prev = refs[j-1]
						break
					}
				}
				var from, to [3]float32
				if !detour.DtStatusFailed(q.snapshot.mesh.GetOffMeshConnectionPolyEndPoints(prev, ref, from[:], to[:])) {
					fw, tw := q.world(from), q.world(to)
					c.Link = &Traversal{ID: l.ID, From: fw, To: tw, Reverse: distance2(fw, l.End) < distance2(fw, l.Start)}
				}
			}
		}
		p.corners = append(p.corners, c)
	}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL) && count > 0 {
		ref := cornRefs[count-1]
		if ref != 0 {
			p.end = Surface{q.snapshot, ref, p.corners[count-1].Position}
		}
	}
	return PathResult{status, p}
}
func distance2(a, b Vec3) float64 {
	return (a.X-b.X)*(a.X-b.X) + (a.Y-b.Y)*(a.Y-b.Y) + (a.Z-b.Z)*(a.Z-b.Z)
}

// Move constrains one local displacement; large movements should be subdivided
// by the host. It never teleports to another floor by nearest-surface search.
func (q *Query) Move(ctx context.Context, start Surface, target Vec3, f Filter) MoveResult {
	if st := q.begin(ctx); st.Code != Complete {
		return MoveResult{Status: st}
	}
	defer q.end()
	if !q.valid(start) {
		return MoveResult{Status: failure(Stale, "navmesh: stale movement surface")}
	}
	if !target.Finite() || !q.representable(target) {
		return MoveResult{Status: failure(InvalidInput, "navmesh: invalid movement target")}
	}
	filter, err := f.detour()
	if err != nil {
		return MoveResult{Status: failure(InvalidInput, err.Error())}
	}
	a, b := q.local(start.position), q.local(target)
	if !q.passes(start.ref, filter) {
		return MoveResult{Status: failure(NoSurface, "navmesh: starting surface excluded by filter")}
	}
	var pos [3]float32
	refs := make([]detour.DtPolyRef, q.limits.PathPolygons)
	var n int
	var hit bool
	st := q.query.MoveAlongSurface(start.ref, a[:], b[:], filter, pos[:], refs, &n, len(refs), &hit)
	if detour.DtStatusFailed(st) || n == 0 {
		return MoveResult{Status: failure(Failed, "navmesh: surface movement failed")}
	}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL) {
		// Detour's visited list is a start-side prefix, while its result position
		// may be farther ahead. Keep the returned position on the retained prefix.
		if detour.DtStatusFailed(q.query.ClosestPointOnPoly(refs[n-1], pos[:], pos[:], nil)) {
			return MoveResult{Status: failure(Failed, "navmesh: truncated movement clamp failed")}
		}
	}
	var y float32
	if detour.DtStatusFailed(q.query.GetPolyHeight(refs[n-1], pos[:], &y)) {
		return MoveResult{Status: failure(NoSurface, "navmesh: movement height unavailable")}
	}
	pos[1] = y
	out := MoveResult{Surface: Surface{q.snapshot, refs[n-1], q.world(pos)}, Normal: q.normal(refs[n-1], pos)}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL|detour.DT_OUT_OF_NODES) {
		out.Status = Status{Code: Partial, Reason: ResourceExhausted}
	}
	for _, r := range refs[:n] {
		var nearest [3]float32
		if !detour.DtStatusFailed(q.query.ClosestPointOnPoly(r, pos[:], nearest[:], nil)) {
			out.Visited = append(out.Visited, Surface{q.snapshot, r, q.world(nearest)})
		}
	}
	return out
}

// Height samples only the selected surface, preserving stacked-floor identity.
func (q *Query) Height(ctx context.Context, s Surface) HeightResult {
	if st := q.begin(ctx); st.Code != Complete {
		return HeightResult{Status: st}
	}
	defer q.end()
	if !q.valid(s) {
		return HeightResult{Status: failure(Stale, "navmesh: stale surface")}
	}
	p := q.local(s.position)
	var h float32
	if detour.DtStatusFailed(q.query.GetPolyHeight(s.ref, p[:], &h)) {
		return HeightResult{Status: failure(NoSurface, "navmesh: height unavailable")}
	}
	return HeightResult{Height: float64(h) + q.snapshot.bundle.Settings.Origin.Y}
}

// Raycast traces walkability on the starting floor; the target Y is not a
// three-dimensional visibility test.
func (q *Query) Raycast(ctx context.Context, s Surface, target Vec3, f Filter) RaycastResult {
	if st := q.begin(ctx); st.Code != Complete {
		return RaycastResult{Status: st}
	}
	defer q.end()
	if !q.valid(s) {
		return RaycastResult{Status: failure(Stale, "navmesh: stale surface")}
	}
	if !target.Finite() || !q.representable(target) {
		return RaycastResult{Status: failure(InvalidInput, "navmesh: invalid ray")}
	}
	filter, err := f.detour()
	if err != nil {
		return RaycastResult{Status: failure(InvalidInput, err.Error())}
	}
	a, b := q.local(s.position), q.local(target)
	if !q.passes(s.ref, filter) {
		return RaycastResult{Status: failure(NoSurface, "navmesh: starting surface excluded by filter")}
	}
	refs := make([]detour.DtPolyRef, q.limits.PathPolygons)
	hit := detour.DtRaycastHit{Path: refs, MaxPath: int32(len(refs))}
	st := q.query.Raycast2(s.ref, a[:], b[:], filter, 0, &hit, 0)
	if detour.DtStatusFailed(st) {
		return RaycastResult{Status: failure(Failed, "navmesh: raycast failed")}
	}
	t := math.Min(1, float64(hit.T))
	out := RaycastResult{Hit: t < 1, Fraction: t, Position: Vec3{X: s.position.X + (target.X-s.position.X)*t, Y: s.position.Y, Z: s.position.Z + (target.Z-s.position.Z)*t}, Normal: Vec3{X: float64(hit.HitNormal[0]), Y: float64(hit.HitNormal[1]), Z: float64(hit.HitNormal[2])}}
	if hit.LastRef != 0 {
		p := q.local(out.Position)
		var h float32
		if !detour.DtStatusFailed(q.query.GetPolyHeight(hit.LastRef, p[:], &h)) {
			out.Position.Y = float64(h) + q.snapshot.bundle.Settings.Origin.Y
		} else {
			out.Status = failure(NoSurface, "navmesh: raycast endpoint height unavailable")
			return out
		}
	}
	if detour.DtStatusDetail(st, detour.DT_BUFFER_TOO_SMALL|detour.DT_OUT_OF_NODES) {
		out.Status = Status{Code: Partial, Reason: ResourceExhausted}
	}
	return out
}
