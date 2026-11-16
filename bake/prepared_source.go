package bake

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// PreparedSource owns validated, instanced geometry for one exact origin and
// strictness policy. It is safe to share across concurrent builds; copies of the
// handle share the same immutable data and lazily constructed spatial index.
// Shared assets and their local indexes are retained once, while transformed
// triangles are materialized only for selected tiles. It retains no runtime snapshots.
// Its zero value is not a prepared source.
type PreparedSource struct{ data *preparedSourceData }

type preparedSourceData struct {
	origin    [3]uint64
	strict    bool
	resolved  geom.Resolved
	geometry  *geom.PreparedScene
	links     []geom.Link
	indexOnce sync.Once
	index     *geom.PreparedIndex
}

// PrepareSource validates and privately copies a scene without building an
// index or navigation mesh. Later scene mutations cannot affect the result.
// Resolution is synchronous; large scenes should be prepared off the tick.
func PrepareSource(scene geom.Scene, origin geom.Vec3, strict bool) (*PreparedSource, error) {
	geometry, err := scene.Prepare(origin, strict)
	if err != nil {
		return nil, err
	}
	links := copySourceLinks(scene.Links)
	// World-space metadata includes disabled links; resolved links are local.
	sortLinks(links)
	return &PreparedSource{data: &preparedSourceData{origin: sourceOriginBits(origin), strict: strict, resolved: geometry.Metadata(), geometry: geometry, links: links}}, nil
}

func sourceOriginBits(origin geom.Vec3) [3]uint64 {
	return [3]uint64{math.Float64bits(origin.X), math.Float64bits(origin.Y), math.Float64bits(origin.Z)}
}

func copySourceLinks(links []geom.Link) []geom.Link {
	owned := append([]geom.Link(nil), links...)
	for i := range owned {
		owned[i].Profiles = append([]string(nil), owned[i].Profiles...)
	}
	return owned
}

// SourceFingerprint identifies the validated authored source, or is empty for
// an uninitialized handle. It does not expose retained geometry.
func (s *PreparedSource) SourceFingerprint() string {
	if s == nil || s.data == nil {
		return ""
	}
	return s.data.resolved.Fingerprint
}

// ResolvedFingerprint identifies the resolved geometry, or is empty for an
// uninitialized handle. Coordinates are relative to the prepared origin.
func (s *PreparedSource) ResolvedFingerprint() string {
	if s == nil || s.data == nil {
		return ""
	}
	return s.data.resolved.ResolvedFingerprint
}

// Objects returns independently owned summaries of every placement with valid
// triangles, in resolution order, without expanding their triangles.
func (s *PreparedSource) Objects() []geom.ResolvedObject {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data.geometry.Objects()
}

// TriangleCount returns the number of resolved triangles without allocating.
func (s *PreparedSource) TriangleCount() int {
	if s == nil || s.data == nil {
		return 0
	}
	return s.data.geometry.TriangleCount()
}

// TriangleAt returns one resolved triangle entirely by value. No returned
// vertex or bounds can mutate the prepared source. Invalid indices return false.
func (s *PreparedSource) TriangleAt(index int) (geom.ResolvedTriangle, bool) {
	if s == nil || s.data == nil || index < 0 || index >= s.data.geometry.TriangleCount() {
		return geom.ResolvedTriangle{}, false
	}
	return s.data.geometry.TriangleAt(index)
}

// BuildTiles bakes selected columns from this immutable source; nil keys means
// the complete region. Origin must match bit-for-bit, including signed zero,
// and Strict must match. Every build still validates its configuration, limits,
// precision and output artifact. Results independently own all public arrays.
// The first nonempty build constructs an immutable spatial index once. That
// construction is synchronous; cancellation is checked before and after it.
func (s *PreparedSource) BuildTiles(ctx context.Context, c Config, keys []artifact.TileKey) (*Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("bake: nil context")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.data == nil {
		return nil, fmt.Errorf("bake: uninitialized prepared source")
	}
	data := s.data
	if data.origin != sourceOriginBits(c.Settings.Origin) || data.strict != c.Strict {
		return nil, fmt.Errorf("bake: prepared source origin/strictness mismatch")
	}
	fingerprint, err := c.Fingerprint()
	if err != nil {
		return nil, err
	}
	if c.Revision == 0 {
		c.Revision = 1
	}
	regionKeys, err := TileKeys(c)
	if err != nil {
		return nil, err
	}
	validKeys := make(map[artifact.TileKey]bool, len(regionKeys))
	for _, key := range regionKeys {
		validKeys[key] = true
	}
	if keys == nil {
		keys = regionKeys
	}
	maxTiles := c.MaxTiles
	if maxTiles == 0 {
		maxTiles = 65536
	}
	if len(keys) > maxTiles {
		return nil, fmt.Errorf("bake: tile count exceeds %d", maxTiles)
	}
	seen := make(map[artifact.TileKey]bool, len(keys))
	for _, key := range keys {
		if !validKeys[key] || seen[key] {
			return nil, fmt.Errorf("bake: duplicate or out-of-region column %v", key)
		}
		seen[key] = true
	}
	if len(keys) > 0 {
		data.indexOnce.Do(func() { data.index = data.geometry.NewIndex() })
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := &Result{Bundle: artifact.Bundle{WorldID: c.WorldID, Revision: c.Revision, Profile: c.Profile, Settings: c.Settings,
		SourceFingerprint: data.resolved.Fingerprint, ResolvedFingerprint: data.resolved.ResolvedFingerprint,
		SettingsFingerprint: fingerprint, Links: copySourceLinks(data.links)}, Sources: append([]geom.Diagnostic(nil), data.resolved.Diagnostics...)}
	for _, key := range keys {
		start := time.Now()
		t, d, e := buildTileGeometry(ctx, data.resolved.Volumes, data.index.Visit, c, key, data.links)
		if e != nil {
			return nil, fmt.Errorf("bake: tile %v: %w", key, e)
		}
		d.Duration = time.Since(start)
		r.Bundle.Tiles = append(r.Bundle.Tiles, t)
		r.Diagnostics = append(r.Diagnostics, d)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.Bundle.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}
