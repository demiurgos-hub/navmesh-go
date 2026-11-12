package dynamic

import (
	"math"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// Safety is immutable metadata for one coherent committed/desired state. Dirty,
// unloaded and leased retired-generation changes quarantine all floors,
// including the bake source border. Old-view movement is safe outside this
// union even after publication while a host incrementally rebases its readers.
// It retains dependency metadata rather than a runtime snapshot or lease.
type Safety struct {
	committed, desired, token uint64
	topology                  navmesh.Topology
	columns                   map[artifact.TileKey]bool
	routeColumns              map[artifact.TileKey]bool
	bounds                    []geom.Bounds
	origin                    geom.Vec3
	tileWidth                 float64
	scopeRevision             uint64
	scopeTopologies           []navmesh.Topology
	scopes                    map[uint64]*Safety
}

// Safety returns a cached, coherent capture suitable for allocation-free host
// movement checks. Later edits/publications never mutate an earlier capture.
func (w *World) Safety() *Safety {
	if cached := w.safety.Load(); cached != nil {
		return cached
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if cached := w.safety.Load(); cached != nil {
		return cached
	}
	base := make(map[artifact.TileKey]bool, len(w.dirty)+len(w.unavailable))
	for key := range w.dirty {
		base[artifact.TileKey{X: key.X, Z: key.Z}] = true
	}
	for key := range w.unavailable {
		base[artifact.TileKey{X: key.X, Z: key.Z}] = true
	}
	s := w.newSafetyLocked(base, nil, 0)
	for _, columns := range w.retired {
		for key := range columns {
			s.columns[key] = true
		}
	}
	s.scopes = make(map[uint64]*Safety, len(w.leases)+1)
	currentScope := w.newSafetyLocked(base, nil, w.current.Revision())
	currentScope.scopeTopologies = []navmesh.Topology{w.current.Topology()}
	s.scopes[w.current.Revision()] = currentScope
	for snapshot, count := range w.leases {
		if count == 0 || snapshot == w.current {
			continue
		}
		revision := snapshot.Revision()
		if scope := s.scopes[revision]; scope != nil {
			for key := range w.retired[snapshot] {
				scope.columns[key] = true
			}
			scope.scopeTopologies = append(scope.scopeTopologies, snapshot.Topology())
		} else {
			scope := w.newSafetyLocked(base, w.retired[snapshot], revision)
			scope.scopeTopologies = []navmesh.Topology{snapshot.Topology()}
			s.scopes[revision] = scope
		}
	}
	s.buildBounds(w.config)
	for _, scope := range s.scopes {
		scope.buildBounds(w.config)
	}
	w.safety.Store(s)
	return s
}

func (w *World) newSafetyLocked(base, extra map[artifact.TileKey]bool, revision uint64) *Safety {
	// Scope is part of safety identity: switching generations must invalidate
	// host caches even when no publication happened between the two checks.
	w.safetyToken++
	s := &Safety{committed: w.current.Revision(), desired: w.revision, token: w.safetyToken, topology: w.current.Topology(), columns: make(map[artifact.TileKey]bool, len(base)+len(extra)), origin: w.config.Settings.Origin, tileWidth: w.config.Settings.CellSize * float64(w.config.Settings.TileSize), scopeRevision: revision}
	for key := range base {
		s.columns[key] = true
	}
	for key := range extra {
		s.columns[key] = true
	}
	return s
}

func (s *Safety) buildBounds(config bake.Config) {
	s.bounds = make([]geom.Bounds, 0, len(s.columns))
	for key := range s.columns {
		s.bounds = append(s.bounds, bake.TileBounds(config, key))
	}
	s.buildRouteColumns(config)
}

// ForRevision selects cached safety for the current or a managed leased
// generation in this immutable capture. It includes pending/unloaded columns
// plus changes published since that generation. Unknown revisions return nil,
// whose safety checks reject use. The caller must keep its lease alive and
// recapture after changes; no snapshot or lease is retained by this metadata.
// Handles must belong to an exact captured snapshot, not merely match metadata.
// Repeated publication without a desired change can share a revision; such a
// scope recognizes the bounded set of actual current/leased snapshot identities.
func (s *Safety) ForRevision(revision uint64) *Safety {
	if s == nil || revision == 0 {
		return nil
	}
	if s.scopeRevision != 0 {
		if s.scopeRevision == revision {
			return s
		}
		return nil
	}
	return s.scopes[revision]
}

// ForEachScope visits each cached generation scope without allocating or
// retaining a runtime snapshot. Order is unspecified; the number of scopes is
// bounded by managed generations. A scoped capture visits only itself. Nil
// captures or visitors do nothing. Captures remain immutable during callbacks.
func (s *Safety) ForEachScope(visit func(revision uint64, scope *Safety)) {
	if s == nil || visit == nil {
		return
	}
	if s.scopeRevision != 0 {
		visit(s.scopeRevision, s)
		return
	}
	for revision, scope := range s.scopes {
		visit(revision, scope)
	}
}

func (w *World) protectRetiredLocked(changed []artifact.TileKey) {
	if len(changed) == 0 {
		return
	}
	if w.retired == nil {
		w.retired = make(map[*navmesh.Snapshot]map[artifact.TileKey]bool)
	}
	// Every leased generation predates this publication. Add the changes to
	// each generation's history so releasing one cannot uncover a column still
	// unsafe to another. The existing generation limit bounds these histories.
	for snapshot, count := range w.leases {
		if count == 0 {
			continue
		}
		columns := w.retired[snapshot]
		if columns == nil {
			columns = make(map[artifact.TileKey]bool, len(changed))
			w.retired[snapshot] = columns
		}
		for _, key := range changed {
			columns[artifact.TileKey{X: key.X, Z: key.Z}] = true
		}
	}
}

func (w *World) invalidateSafetyLocked() {
	// Captured fields have already been changed under mu. A fast reader that
	// loaded the prior immutable pointer linearizes before this invalidation;
	// subsequent misses wait for the mutation to finish before capturing again.
	w.safety.Store(nil)
	w.safetyToken++
}

// CommittedRevision identifies the published geometry used by this capture.
func (s *Safety) CommittedRevision() uint64 {
	if s == nil {
		return 0
	}
	return s.committed
}

// DesiredRevision identifies the accepted desired geometry in this capture.
func (s *Safety) DesiredRevision() uint64 {
	if s == nil {
		return 0
	}
	return s.desired
}

// Token changes on desired mutation, publication or release of the last lease
// contributing retired-generation quarantine, independently of build state.
func (s *Safety) Token() uint64 {
	if s == nil {
		return 0
	}
	return s.token
}

// ProtectsRetiredViews reports whether this capture protects managed old leases
// from all changes published since their snapshots were current.
func (s *Safety) ProtectsRetiredViews() bool { return s != nil }

// HasQuarantine reports pending, unloaded or retained-generation quarantine.
// Equal committed/desired revisions do not imply that old leases have drained.
func (s *Safety) HasQuarantine() bool { return s == nil || len(s.columns) != 0 }

// SafeLocation checks retained support identity and the full quarantine union.
func (s *Safety) SafeLocation(location navmesh.Surface) bool {
	if s == nil || s.scopeRevision != 0 && location.Revision() != s.scopeRevision {
		return false
	}
	var tile navmesh.TileRevision
	var ok bool
	if s.scopeRevision != 0 {
		for _, topology := range s.scopeTopologies {
			if topology.OwnsSurface(location) {
				tile, ok = location.Tile()
				break
			}
		}
	} else {
		tile, ok = s.topology.UnchangedSurfaceTile(location)
	}
	return ok && (len(s.columns) == 0 || !s.columns[artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}] && s.SafeSegment(location.Position(), location.Position()))
}

// SafePath checks all retained corridor dependencies without copying them.
// Every dependency's full tile column must avoid the padded quarantine region.
func (s *Safety) SafePath(path *navmesh.Path) bool {
	if s == nil || s.scopeRevision != 0 && path.Revision() != s.scopeRevision {
		return false
	}
	if s.scopeRevision != 0 {
		// Exact captured views are protected by all changes since that view.
		// Quarantine below replaces repeated comparison with latest topology.
		owned := false
		for _, topology := range s.scopeTopologies {
			owned = owned || topology.OwnsPath(path)
		}
		if !owned {
			return false
		}
	} else if !s.topology.PathUnchanged(path) {
		return false
	}
	if len(s.routeColumns) == 0 {
		return true
	}
	for i := 0; i < path.TileCount(); i++ {
		tile, _ := path.TileAt(i)
		if s.routeColumns[artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}] {
			return false
		}
		min := geom.Vec3{X: s.origin.X + float64(tile.Key.X)*s.tileWidth, Z: s.origin.Z + float64(tile.Key.Z)*s.tileWidth}
		max := geom.Vec3{X: s.origin.X + (float64(tile.Key.X)+1)*s.tileWidth, Z: s.origin.Z + (float64(tile.Key.Z)+1)*s.tileWidth}
		if !(geom.Bounds{Min: min, Max: max}).Valid() {
			return false
		}
	}
	return true
}

// SafeBounds rejects any intersection with conservative quarantined columns.
// This is a pending-geometry test, not a walkability or physical collision test.
func (s *Safety) SafeBounds(min, max geom.Vec3) bool {
	if s == nil || !(geom.Bounds{Min: min, Max: max}).Valid() {
		return false
	}
	for _, b := range s.bounds {
		if min.X <= b.Max.X && max.X >= b.Min.X && min.Z <= b.Max.Z && max.Z >= b.Min.Z {
			return false
		}
	}
	return true
}

// SafeSegment checks every point of a center segment against the padded dirty
// columns. Vertical separation never bypasses a column's quarantine.
func (s *Safety) SafeSegment(from, to geom.Vec3) bool {
	if s == nil || !from.Finite() || !to.Finite() {
		return false
	}
	dx, dz := to.X-from.X, to.Z-from.Z
	if math.IsInf(dx, 0) || math.IsInf(dz, 0) {
		return false
	}
	minX, maxX := math.Min(from.X, to.X), math.Max(from.X, to.X)
	minZ, maxZ := math.Min(from.Z, to.Z), math.Max(from.Z, to.Z)
	for _, b := range s.bounds {
		if maxX < b.Min.X || minX > b.Max.X || maxZ < b.Min.Z || minZ > b.Max.Z {
			continue
		}
		lo, hi := 0.0, 1.0
		if clipSegment(from.X, dx, b.Min.X, b.Max.X, &lo, &hi) && clipSegment(from.Z, dz, b.Min.Z, b.Max.Z, &lo, &hi) {
			return false
		}
	}
	return true
}

func clipSegment(start, delta, min, max float64, lo, hi *float64) bool {
	if delta == 0 {
		return start >= min && start <= max
	}
	a, b := (min-start)/delta, (max-start)/delta
	if a > b {
		a, b = b, a
	}
	*lo = math.Max(*lo, a)
	*hi = math.Min(*hi, b)
	return *lo <= *hi
}
