package navmesh

import (
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// Nonzero size guarantees distinct identities for live snapshot allocations.
// Metadata may retain this byte without retaining its snapshot or runtime mesh.
type snapshotIdentity struct{ marker byte }

// A snapshot builds this immutable table by runtime tile slot. The reference
// range includes its exact salt and all ground/off-mesh polygons. Values retain
// no mesh or snapshot pointers; Surface itself needs no extra cache field.
type surfaceTileDependency struct {
	tile  TileRevision
	first detour.DtPolyRef
	count uint32
}

// Topology is immutable dependency metadata. It does not retain a runtime mesh,
// so caching it does not extend the lifetime of a managed snapshot generation.
// Its private identity distinguishes the exact source snapshot independently of
// world/revision numbers; other snapshots still require dependency validation.
type Topology struct {
	identity          *snapshotIdentity
	worldID, settings string
	profile           geom.Profile
	configuration     artifact.Settings
	tiles             map[artifact.TileKey]uint64
}

// Topology returns lightweight, immutable dependency metadata for this snapshot.
func (s *Snapshot) Topology() Topology {
	if s == nil {
		return Topology{}
	}
	return Topology{identity: s.identity, worldID: s.bundle.WorldID, settings: s.bundle.SettingsFingerprint, profile: s.bundle.Profile, configuration: s.bundle.Settings, tiles: s.tiles}
}

// OwnsSurface reports whether a nonzero finite handle belongs to this exact
// captured snapshot identity. Equal world/revision metadata is insufficient.
// It does not validate filter eligibility, quarantine or lease lifetime.
func (t Topology) OwnsSurface(s Surface) bool {
	return s.Valid() && s.position.Finite() && t.sameSnapshot(s.snapshot)
}

// OwnsPath reports whether a nonempty path belongs to this exact captured
// snapshot identity without walking its dependencies. It does not establish
// safety against later changes, filter eligibility or lease lifetime.
func (t Topology) OwnsPath(p *Path) bool {
	return p != nil && len(p.refs) != 0 && len(p.tiles) != 0 && t.sameSnapshot(p.snapshot)
}

// Revision identifies the immutable snapshot generation that owns this path.
// A nil path has revision zero.
func (p *Path) Revision() uint64 {
	if p == nil || p.snapshot == nil {
		return 0
	}
	return p.snapshot.Revision()
}

func (t Topology) compatible(s *Snapshot) bool {
	return s != nil && t.tiles != nil && (t.sameSnapshot(s) || t.worldID == s.bundle.WorldID &&
		t.settings == s.bundle.SettingsFingerprint && t.profile == s.bundle.Profile && t.configuration == s.bundle.Settings)
}

func (t Topology) sameSnapshot(s *Snapshot) bool {
	return s != nil && t.identity != nil && t.identity == s.identity
}

// Tile returns this support polygon's tile dependency without allocating.
func (s Surface) Tile() (TileRevision, bool) {
	if !s.Valid() || !s.position.Finite() || s.snapshot.mesh == nil {
		return TileRevision{}, false
	}
	slot := s.snapshot.mesh.DecodePolyIdTile(s.ref)
	if slot < uint32(len(s.snapshot.dependencies)) {
		dependency := &s.snapshot.dependencies[slot]
		// Poly references are unsigned and each tile occupies a contiguous
		// salt-qualified range. Wrong salts and excess polygon indices cannot
		// satisfy this subtraction bound, including wraparound at ref zero.
		if dependency.count != 0 && uint32(s.ref-dependency.first) < dependency.count {
			return dependency.tile, true
		}
	}
	// Raw/internal snapshots without an assembled table, and cache misses,
	// retain the original checked reference and metadata lookup.
	var tile *detour.DtMeshTile
	var poly *detour.DtPoly
	if detour.DtStatusFailed(s.snapshot.mesh.GetTileAndPolyByRef(s.ref, &tile, &poly)) {
		return TileRevision{}, false
	}
	key := artifact.TileKey{X: tile.Header.X, Z: tile.Header.Y, Layer: tile.Header.Layer}
	revision, ok := s.snapshot.tiles[key]
	return TileRevision{Key: key, Revision: revision}, ok
}

// SurfaceUnchanged reports whether a retained support polygon's tile still has
// the same identity and revision. It does not test current filter eligibility.
func (t Topology) SurfaceUnchanged(s Surface) bool {
	_, ok := t.UnchangedSurfaceTile(s)
	return ok
}

// UnchangedSurfaceTile validates retained support and returns its unchanged tile
// dependency in one lookup. It does not test current filter eligibility.
func (t Topology) UnchangedSurfaceTile(s Surface) (TileRevision, bool) {
	if !t.compatible(s.snapshot) {
		return TileRevision{}, false
	}
	tile, ok := s.Tile()
	if !ok {
		return TileRevision{}, false
	}
	if t.sameSnapshot(s.snapshot) {
		return tile, true
	}
	revision, ok := t.tiles[tile.Key]
	return tile, ok && revision == tile.Revision
}

// PathUnchanged reports whether every retained corridor dependency is current.
func (t Topology) PathUnchanged(p *Path) bool {
	if p == nil || len(p.tiles) == 0 || !t.compatible(p.snapshot) {
		return false
	}
	if t.sameSnapshot(p.snapshot) {
		return true
	}
	for _, tile := range p.tiles {
		if revision, ok := t.tiles[tile.Key]; !ok || revision != tile.Revision {
			return false
		}
	}
	return true
}
