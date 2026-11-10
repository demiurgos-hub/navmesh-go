// Package bake runs the complete pure-Go geometry-to-navigation pipeline.
package bake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/recast"
)

// Config identifies one fixed region/profile build. Bounds are world-space;
// tile coordinates are signed floor coordinates relative to Settings.Origin.
type Config struct {
	WorldID                       string
	Revision                      uint64
	Profile                       geom.Profile
	Settings                      artifact.Settings
	Strict                        bool
	MaxTiles, MaxTrianglesPerTile int
	// MaxDetailSamplesPerTile bounds detail sampling work; zero uses Recast's default.
	MaxDetailSamplesPerTile int
	AreaFlags               [64]uint16
}

// TileDiagnostic records stage warnings and elapsed tile time.
type TileDiagnostic struct {
	Key       artifact.TileKey
	Duration  time.Duration
	Messages  []recast.Diagnostic
	Triangles int
	Stats     recast.BuildStats
}

// Result owns a complete portable bundle and bake diagnostics.
type Result struct {
	Bundle      artifact.Bundle
	Diagnostics []TileDiagnostic
	Sources     []geom.Diagnostic
}

// DefaultConfig supplies explicit demonstration settings, not game requirements.
func DefaultConfig(world string, bounds geom.Bounds) Config {
	return Config{WorldID: world, Revision: 1, Profile: geom.Profile{ID: "human", Radius: .4, Height: 1.8, MaxSlope: 45, MaxClimb: .4}, Settings: artifact.Settings{Bounds: bounds, CellSize: .2, CellHeight: .1, TileSize: 64, Partition: "layers", MinRegionArea: 0, MergeRegionArea: 400, MaxEdgeLength: 60, MaxSimplificationError: 1.3, DetailSampleDistance: 1.2, DetailSampleMaxError: .1}, MaxTiles: 65536, MaxTrianglesPerTile: 1 << 20}
}

// Fingerprint hashes all settings affecting geometry, excluding transaction
// revision and resource limits. It is stable across equivalent builds.
func (c Config) Fingerprint() (string, error) {
	v := struct {
		World    string
		Profile  geom.Profile
		Settings artifact.Settings
		Flags    [64]uint16
		Strict   bool
	}{c.WorldID, c.Profile, c.Settings, c.AreaFlags, c.Strict}
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Validate rejects invalid or unrepresentable configurations before allocation.
func (c Config) Validate() error {
	b := artifact.Bundle{WorldID: c.WorldID, Profile: c.Profile, Settings: c.Settings}
	if err := b.Validate(); err != nil {
		return err
	}
	s := c.Settings
	if s.Bounds.Min.X == s.Bounds.Max.X || s.Bounds.Min.Z == s.Bounds.Max.Z || s.Bounds.Min.Y == s.Bounds.Max.Y {
		return fmt.Errorf("bake: region requires positive extent on every axis")
	}
	if (s.Bounds.Max.Y-s.Bounds.Min.Y)/s.CellHeight > 8191 {
		return fmt.Errorf("bake: vertical span exceeds 8191 cells; partition vertical regions")
	}
	if math.Ceil(c.Profile.Height/s.CellHeight) > 255 {
		return fmt.Errorf("bake: agent height exceeds compact clearance capacity")
	}
	if math.Ceil(c.Profile.Radius/s.CellSize) > 127 {
		return fmt.Errorf("bake: radius exceeds erosion distance capacity")
	}
	if s.BuildLayers && s.TileSize > 255 {
		return fmt.Errorf("bake: compressed layers require tile size <=255")
	}
	if c.MaxTiles < 0 || c.MaxTrianglesPerTile < 0 || c.MaxDetailSamplesPerTile < 0 {
		return fmt.Errorf("bake: invalid resource limit")
	}
	return nil
}

// Build bakes every tile in a region, including explicit empty tiles.
func Build(ctx context.Context, scene geom.Scene, c Config) (*Result, error) {
	return BuildTiles(ctx, scene, c, nil)
}

// BuildTiles bakes only requested tile columns; nil means the complete region.
// It always rebuilds every vertical span in each selected column.
func BuildTiles(ctx context.Context, scene geom.Scene, c Config, keys []artifact.TileKey) (*Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("bake: nil context")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := scene.Resolve(c.Settings.Origin, c.Strict)
	if err != nil {
		return nil, err
	}
	fingerprint, err := c.Fingerprint()
	if err != nil {
		return nil, err
	}
	if c.Revision == 0 {
		c.Revision = 1
	}
	r := &Result{Bundle: artifact.Bundle{WorldID: c.WorldID, Revision: c.Revision, Profile: c.Profile, Settings: c.Settings, SourceFingerprint: resolved.Fingerprint, ResolvedFingerprint: resolved.ResolvedFingerprint, SettingsFingerprint: fingerprint}, Sources: resolved.Diagnostics}
	// Link keys depend on stable IDs, never slice order or profile eligibility.
	index := geom.NewIndex(resolved.Triangles)
	allLinks := append([]geom.Link(nil), scene.Links...)
	sortLinks(allLinks)
	r.Bundle.Links = allLinks
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
	seen := map[artifact.TileKey]bool{}
	for _, key := range keys {
		if !validKeys[key] || seen[key] {
			return nil, fmt.Errorf("bake: duplicate or out-of-region column %v", key)
		}
		seen[key] = true
		start := time.Now()
		t, d, e := buildTile(ctx, resolved, index, c, key, allLinks)
		if e != nil {
			return nil, fmt.Errorf("bake: tile %v: %w", key, e)
		}
		d.Duration = time.Since(start)
		r.Bundle.Tiles = append(r.Bundle.Tiles, t)
		r.Diagnostics = append(r.Diagnostics, d)
	}
	if err := r.Bundle.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}
func sortLinks(a []geom.Link) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].ID < a[j-1].ID; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// TileKeys returns deterministic row-major keys covering the configured region.
// Invalid configurations return an error before grid arithmetic or allocation.
func TileKeys(c Config) ([]artifact.TileKey, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	s := c.Settings
	tw := s.CellSize * float64(s.TileSize)
	x0, x1 := math.Floor((s.Bounds.Min.X-s.Origin.X)/tw), math.Ceil((s.Bounds.Max.X-s.Origin.X)/tw)
	z0, z1 := math.Floor((s.Bounds.Min.Z-s.Origin.Z)/tw), math.Ceil((s.Bounds.Max.Z-s.Origin.Z)/tw)
	limit := c.MaxTiles
	if limit == 0 {
		limit = 65536
	}
	if !s.Bounds.Valid() || tw <= 0 || x0 < math.MinInt32 || z0 < math.MinInt32 || x1 > math.MaxInt32 || z1 > math.MaxInt32 || (x1-x0)*(z1-z0) > float64(limit) {
		return nil, fmt.Errorf("bake: tile coordinate/count limit")
	}
	out := make([]artifact.TileKey, 0, int((x1-x0)*(z1-z0)))
	for z := int64(z0); z < int64(z1); z++ {
		for x := int64(x0); x < int64(x1); x++ {
			out = append(out, artifact.TileKey{X: int32(x), Z: int32(z)})
		}
	}
	return out, nil
}

// TileBounds returns a tile's padded source-selection box in world coordinates.
func TileBounds(c Config, key artifact.TileKey) geom.Bounds {
	s := c.Settings
	tw := s.CellSize * float64(s.TileSize)
	padding := (math.Ceil(c.Profile.Radius/s.CellSize) + 3) * s.CellSize
	return geom.Bounds{Min: geom.Vec3{X: s.Origin.X + float64(key.X)*tw - padding, Y: s.Bounds.Min.Y, Z: s.Origin.Z + float64(key.Z)*tw - padding}, Max: geom.Vec3{X: s.Origin.X + (float64(key.X)+1)*tw + padding, Y: s.Bounds.Max.Y, Z: s.Origin.Z + (float64(key.Z)+1)*tw + padding}}
}
func buildTile(ctx context.Context, r geom.Resolved, index *geom.Index, c Config, key artifact.TileKey, links []geom.Link) (artifact.Tile, TileDiagnostic, error) {
	tile := artifact.Tile{Key: key, Revision: c.Revision, Empty: true}
	diag := TileDiagnostic{Key: key}
	if err := ctx.Err(); err != nil {
		return tile, diag, err
	}
	s := c.Settings
	box := TileBounds(c, key)
	box.Min = sub(box.Min, s.Origin)
	box.Max = sub(box.Max, s.Origin)
	radius := int(math.Ceil(c.Profile.Radius / s.CellSize))
	border := radius + 3
	size := int(s.TileSize) + 2*border
	cfg := recast.Config{Width: size, Height: size, TileSize: int(s.TileSize), BorderSize: border, Cs: float32(s.CellSize), Ch: float32(s.CellHeight), Bmin: vec(box.Min), Bmax: vec(box.Max), WalkableSlopeAngle: float32(c.Profile.MaxSlope), WalkableHeight: int(math.Ceil(c.Profile.Height / s.CellHeight)), WalkableClimb: int(math.Floor(c.Profile.MaxClimb / s.CellHeight)), WalkableRadius: radius, MaxEdgeLen: int(s.MaxEdgeLength), MaxSimplificationError: float32(s.MaxSimplificationError), MinRegionArea: int(s.MinRegionArea), MergeRegionArea: int(s.MergeRegionArea), MaxVertsPerPoly: 6, DetailSampleDist: float32(s.DetailSampleDistance), DetailSampleMaxError: float32(s.DetailSampleMaxError)}
	var verts []float32
	var tris []int
	var areas []uint8
	maxTri := c.MaxTrianglesPerTile
	if maxTri == 0 {
		maxTri = 1 << 20
	}
	precision := math.Min(s.CellSize, s.CellHeight) / 16
	for _, idx := range index.Query(box) {
		tr := r.Triangles[idx]
		if len(areas) >= maxTri {
			return tile, diag, fmt.Errorf("triangle limit %d exceeded", maxTri)
		}
		for _, p := range tr.Vertices {
			for _, v := range []float64{p.X, p.Y, p.Z} {
				f := float32(v)
				if math.IsInf(float64(f), 0) || math.Abs(float64(math.Nextafter32(f, float32(math.Inf(1))))-float64(f)) > precision {
					return tile, diag, fmt.Errorf("object %q exceeds float32 precision envelope", tr.ObjectID)
				}
			}
			tris = append(tris, len(verts)/3)
			verts = append(verts, float32(p.X), float32(p.Y), float32(p.Z))
		}
		areas = append(areas, tr.Area)
	}
	diag.Triangles = len(areas)
	mode := recast.Monotone
	if s.Partition == "layers" {
		mode = recast.LayerRegions
	} else if s.Partition == "watershed" {
		mode = recast.Watershed
	}
	opt := recast.BuildOptions{Partition: mode, BuildLayers: s.BuildLayers, MaxDetailSamples: c.MaxDetailSamplesPerTile}
	for _, v := range r.Volumes {
		if !box.Intersects(v.Bounds.Expand(geom.Vec3{Y: c.Profile.Height})) {
			continue
		}
		volume := recast.AreaVolume{Shape: recast.BoxArea, Min: vec(v.Bounds.Min), Max: vec(v.Bounds.Max), Radius: float32(v.Radius), Height: float32(v.Bounds.Max.Y - v.Bounds.Min.Y), Area: v.Area}
		if v.Exclude {
			volume.Area = 0
			volume.Min[1] -= float32(c.Profile.Height)
			volume.Height += float32(c.Profile.Height)
		}
		if v.Shape == "cylinder" {
			volume.Shape = recast.CylinderArea
			volume.Min[0] = float32((v.Bounds.Min.X + v.Bounds.Max.X) / 2)
			volume.Min[2] = float32((v.Bounds.Min.Z + v.Bounds.Max.Z) / 2)
		} else if v.Shape == "convex" {
			volume.Shape = recast.ConvexArea
			for _, p := range v.Points {
				volume.Vertices = append(volume.Vertices, float32(p.X), float32(p.Y), float32(p.Z))
			}
		}
		opt.Volumes = append(opt.Volumes, volume)
	}
	out, err := recast.Build(ctx, cfg, verts, tris, areas, opt)
	if err != nil {
		return tile, diag, err
	}
	diag.Messages = out.Diagnostics
	diag.Stats = out.Stats
	if out.Layers != nil {
		for _, l := range out.Layers.Layers {
			tile.Layers = append(tile.Layers, artifact.Layer{BoundsMin: l.Bmin, BoundsMax: l.Bmax, CellSize: l.Cs, CellHeight: l.Ch, Width: int32(l.Width), Height: int32(l.Height), MinX: int32(l.MinX), MaxX: int32(l.MaxX), MinZ: int32(l.MinY), MaxZ: int32(l.MaxY), HMin: int32(l.HMin), HMax: int32(l.HMax), Heights: l.Heights, Areas: l.Areas, Connections: l.Cons})
		}
	}
	m, d := out.Mesh, out.Detail
	if m == nil || m.Npolys == 0 {
		return tile, diag, nil
	}
	tile.Empty = false
	p := detour.DtNavMeshCreateParams{Verts: m.Verts[:m.Nverts*3], VertCount: int32(m.Nverts), Polys: m.Polys[:m.Npolys*m.Nvp*2], PolyFlags: make([]uint16, m.Npolys), PolyAreas: m.Areas[:m.Npolys], PolyCount: int32(m.Npolys), Nvp: int32(m.Nvp), TileX: key.X, TileY: key.Z, TileLayer: key.Layer, Bmin: m.Bmin, Bmax: m.Bmax, WalkableHeight: float32(c.Profile.Height), WalkableRadius: float32(c.Profile.Radius), WalkableClimb: float32(c.Profile.MaxClimb), Cs: m.Cs, Ch: m.Ch, BuildBvTree: true}
	for i, a := range p.PolyAreas {
		p.PolyFlags[i] = c.AreaFlags[a]
		if p.PolyFlags[i] == 0 {
			p.PolyFlags[i] = 1
		}
	}
	if d != nil {
		p.DetailMeshes = d.Meshes[:d.Nmeshes*4]
		p.DetailVerts = d.Verts[:d.Nverts*3]
		p.DetailVertsCount = int32(d.Nverts)
		p.DetailTris = d.Tris[:d.Ntris*4]
		p.DetailTriCount = int32(d.Ntris)
	}
	tw := s.CellSize * float64(s.TileSize)
	for _, l := range links {
		if l.Disabled {
			continue
		}
		eligible := len(l.Profiles) == 0
		for _, id := range l.Profiles {
			if id == c.Profile.ID {
				eligible = true
			}
		}
		if !eligible {
			continue
		}
		a, b := sub(l.Start, s.Origin), sub(l.End, s.Origin)
		if int64(math.Floor(a.X/tw)) != int64(key.X) || int64(math.Floor(a.Z/tw)) != int64(key.Z) {
			continue
		}
		p.OffMeshConVerts = append(p.OffMeshConVerts, float32(a.X), float32(a.Y), float32(a.Z), float32(b.X), float32(b.Y), float32(b.Z))
		p.OffMeshConRad = append(p.OffMeshConRad, float32(l.Radius))
		p.OffMeshConFlags = append(p.OffMeshConFlags, l.Flags)
		p.OffMeshConAreas = append(p.OffMeshConAreas, l.Area)
		direction := uint8(0)
		if l.Bidirectional {
			direction = 1
		}
		p.OffMeshConDir = append(p.OffMeshConDir, direction)
		p.OffMeshConUserID = append(p.OffMeshConUserID, geom.LinkKey(l.ID))
		p.OffMeshConCount++
	}
	tile.Parameters = p
	return tile, diag, nil
}
func vec(v geom.Vec3) [3]float32   { return [3]float32{float32(v.X), float32(v.Y), float32(v.Z)} }
func sub(a, b geom.Vec3) geom.Vec3 { return geom.Vec3{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z} }
