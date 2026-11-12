// Package worldgen constructs deterministic, retained-source qualification worlds.
// These synthetic worlds exercise geometry families; they are not a substitute
// for a consumer's representative content or an agent-capacity guarantee.
package worldgen

import (
	"fmt"
	"math"

	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// Config controls a reproducible world. HalfExtent is the distance from zero
// to either X/Z boundary; 128 therefore creates a 256 by 256 metre world.
type Config struct {
	Family                                     string
	Seed                                       uint64
	HalfExtent, GridStep, CellSize, CellHeight float64
	TileSize                                   int
	// FeatureSpacing repeats static terrain/building features across large worlds.
	// Zero uses 64 metres. Dynamic edits remain local to measure unaffected routes.
	FeatureSpacing float64
}

// Fixture contains source, independently baked clearance profiles, and named
// sample points. Source coordinates and all identifiers are deterministic.
type Fixture struct {
	Version  int
	Config   Config
	Scene    geom.Scene
	Profiles []bake.Config
	// BodyProfiles records physical bodies separately from baked clearance.
	// Version 1 fixtures without this field use the corresponding baked profile.
	BodyProfiles []geom.Profile `json:",omitempty"`
	// BakeClearanceMargin is the additional baked radius used by Capacity only.
	// Its two-cell margin is validated for this fixture, not a general geometric
	// clearance guarantee. Other families keep identical body and bake profiles.
	BakeClearanceMargin                              float64 `json:",omitempty"`
	Anchors                                          []geom.Vec3
	Features                                         []string
	BridgeInstance                                   string
	TraversalStart, TraversalEnd                     geom.Vec3
	UnaffectedTraversalStart, UnaffectedTraversalEnd geom.Vec3
}

// Source is the standalone baker's explicit geometry/configuration contract.
type Source struct {
	Generator string
	Scene     geom.Scene
	Config    bake.Config
}

// DefaultConfig returns the full-size terrain workload configuration.
func DefaultConfig() Config {
	return Config{Family: "terrain", Seed: 1, HalfExtent: 128, GridStep: 4, CellSize: .25, CellHeight: .1, TileSize: 64, FeatureSpacing: 64}
}

// Generate constructs terrain, multilevel, construction, or an opt-in capacity
// corridor. It rejects oversized or malformed settings before allocating arrays.
// Terrain uses a 0.5-voxel contour error validated for its fixture routes, not a
// general clearance guarantee. Its default 1.1-metre gaps block both profiles;
// they do not demonstrate passage for one body size but not the other.
func Generate(c Config) (Fixture, error) {
	if c.FeatureSpacing == 0 {
		c.FeatureSpacing = 64
	}
	f := Fixture{Version: 1, Config: c}
	if c.Family != "terrain" && c.Family != "multilevel" && c.Family != "construction" && c.Family != "capacity" {
		return f, fmt.Errorf("worldgen: unknown family %q", c.Family)
	}
	for _, v := range []float64{c.HalfExtent, c.GridStep, c.CellSize, c.CellHeight} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return f, fmt.Errorf("worldgen: dimensions must be positive and finite")
		}
	}
	if c.HalfExtent < 16 || c.HalfExtent > 2048 || c.GridStep > c.HalfExtent/4 || c.TileSize < 8 || c.TileSize > 255 {
		return f, fmt.Errorf("worldgen: half extent [16,2048], grid <= extent/4 and tile size [8,255] required")
	}
	if c.Family == "capacity" && c.HalfExtent < 64 {
		return f, fmt.Errorf("worldgen: capacity corridor requires half extent >=64 metres")
	}
	if math.IsNaN(c.FeatureSpacing) || math.IsInf(c.FeatureSpacing, 0) || c.FeatureSpacing < 32 {
		return f, fmt.Errorf("worldgen: feature spacing must be finite and >=32 metres")
	}
	n := int(math.Ceil(2 * c.HalfExtent / c.GridStep))
	if n > 1024 {
		return f, fmt.Errorf("worldgen: grid exceeds 1024 cells per side")
	}
	step := 2 * c.HalfExtent / float64(n)
	ground := geom.Asset{ID: "ground"}
	for z := 0; z <= n; z++ {
		for x := 0; x <= n; x++ {
			px, pz := -c.HalfExtent+float64(x)*step, -c.HalfExtent+float64(z)*step
			ground.Vertices = append(ground.Vertices, geom.Vec3{X: px, Y: height(c, px, pz), Z: pz})
		}
	}
	for z := 0; z < n; z++ {
		for x := 0; x < n; x++ {
			a := uint32(z*(n+1) + x)
			ground.Triangles = append(ground.Triangles, geom.Triangle{Indices: [3]uint32{a, a + uint32(n+1), a + 1}, Area: 1}, geom.Triangle{Indices: [3]uint32{a + 1, a + uint32(n+1), a + uint32(n+2)}, Area: 1})
		}
	}
	add(&f, ground, "terrain", geom.Identity())
	// Keep ordinary navigation points away from the edge erosion band.
	for z := 1; z < 8; z++ {
		for x := 1; x < 8; x++ {
			px, pz := c.HalfExtent*(float64(x)/4-1), c.HalfExtent*(float64(z)/4-1)
			f.Anchors = append(f.Anchors, geom.Vec3{X: px, Y: height(c, px, pz), Z: pz})
		}
	}
	f.Features = []string{"negative and positive tile coordinates", "independently retained ground", "two physical clearance profiles", "routes crossing tile seams"}
	switch c.Family {
	case "capacity":
		f.BakeClearanceMargin = 2 * c.CellSize
		f.Features = append(f.Features, "static sixteen-lane serpentine corridor", "long startup routes without destination renewal", "static exclusion-wall collision only; no agent avoidance", "explicit two-cell bake-clearance margin; unchanged physical bodies")
		outer := c.HalfExtent - 4
		first, last := -outer+8, outer-8
		f.Anchors = []geom.Vec3{{X: first, Z: first}, {X: first, Z: last}}
		f.Scene.Volumes = append(f.Scene.Volumes,
			box("capacity-west", geom.Vec3{X: -c.HalfExtent, Y: -2, Z: -c.HalfExtent}, geom.Vec3{X: -outer, Y: 4, Z: c.HalfExtent}),
			box("capacity-east", geom.Vec3{X: outer, Y: -2, Z: -c.HalfExtent}, geom.Vec3{X: c.HalfExtent, Y: 4, Z: c.HalfExtent}),
			box("capacity-south", geom.Vec3{X: -outer, Y: -2, Z: -c.HalfExtent}, geom.Vec3{X: outer, Y: 4, Z: -outer}),
			box("capacity-north", geom.Vec3{X: -outer, Y: -2, Z: outer}, geom.Vec3{X: outer, Y: 4, Z: c.HalfExtent}),
		)
		for i := 0; i < 15; i++ {
			z := first + (float64(i)+.5)*(last-first)/15
			x0, x1 := -outer, outer-8
			if i%2 != 0 {
				x0, x1 = -outer+8, outer
			}
			f.Scene.Volumes = append(f.Scene.Volumes, box(fmt.Sprintf("capacity-divider-%02d", i), geom.Vec3{X: x0, Y: -2, Z: z - 1}, geom.Vec3{X: x1, Y: 4, Z: z + 1}))
		}
	case "terrain":
		f.Features = append(f.Features, "rolling triangle terrain", "sloped ramp", "0.3 metre stairs", "1.1 metre narrow passage")
		add(&f, quad("slope", -10, -6, 10, -2, 0, 5), "slope", geom.Identity())
		for i := 0; i < 10; i++ {
			x := -10 + float64(i)
			add(&f, quad(fmt.Sprintf("step-%02d", i), x, 4, x+1, 8, float64(i)*.3, float64(i)*.3), fmt.Sprintf("stairs-%02d", i), geom.Identity())
		}
		f.Scene.Volumes = append(f.Scene.Volumes, box("passage-left", geom.Vec3{X: -5, Y: -2, Z: 10}, geom.Vec3{X: 5, Y: 7, Z: 12}), box("passage-right", geom.Vec3{X: -5, Y: -2, Z: 13.1}, geom.Vec3{X: 5, Y: 7, Z: 15}))
	case "multilevel":
		f.Features = append(f.Features, "stacked floors", "ramp-connected building floors", "cave ceiling", "bridge above retained ground")
		for i := 1; i <= 3; i++ {
			y := float64(i) * 4
			add(&f, quad(fmt.Sprintf("floor-%d", i), -12, -12, 12, 12, y, y), fmt.Sprintf("building-floor-%d", i), geom.Identity())
			f.Anchors = append(f.Anchors, geom.Vec3{X: 0, Y: y, Z: 0})
		}
		for i := 0; i < 3; i++ {
			y := float64(i) * 4
			// Keep the run outside the floor footprint. Alternating opposite
			// faces avoids headroom loss where reverse runs would intersect.
			z0, z1, landingZ0, landingZ1 := 12.0, 16.0, 8.0, 16.0
			y0, y1 := y, y+4
			if i%2 != 0 {
				z0, z1, landingZ0, landingZ1 = -16, -12, -16, -8
				y0, y1 = y+4, y
			}
			add(&f, quad(fmt.Sprintf("ramp-%d", i), -10, z0, 10, z1, y0, y1), fmt.Sprintf("building-ramp-%d", i), geom.Identity())
			add(&f, quad(fmt.Sprintf("ramp-landing-left-%d", i), -16, landingZ0, -10, landingZ1, y0, y0), fmt.Sprintf("building-landing-left-%d", i), geom.Identity())
			add(&f, quad(fmt.Sprintf("ramp-landing-right-%d", i), 10, landingZ0, 16, landingZ1, y1, y1), fmt.Sprintf("building-landing-right-%d", i), geom.Identity())
		}
		// A solid roof with downward underside creates genuine overhead clearance.
		add(&f, slab("cave-ceiling", geom.Vec3{X: -12, Y: 2.4, Z: -14}, geom.Vec3{X: 12, Y: 3.4, Z: -9}), "cave-ceiling", geom.Identity())
	case "construction":
		f.Features = append(f.Features, "overlapping removable exclusion volumes", "added and removed bridge", "forced off-mesh traversal island", "tile load and unload schedule")
		f.Scene.Volumes = append(f.Scene.Volumes, box("overlap-a", geom.Vec3{X: -3, Y: -1, Z: -2}, geom.Vec3{X: 1, Y: 4, Z: 2}), box("overlap-b", geom.Vec3{X: -1, Y: -1, Z: -2}, geom.Vec3{X: 3, Y: 4, Z: 2}))
		add(&f, quad("island", -3, -3, 3, 3, 15, 15), "traversal-island", geom.Identity())
		f.TraversalStart = geom.Vec3{X: -8}
		f.TraversalEnd = geom.Vec3{X: 0, Y: 15}
		f.Scene.Links = append(f.Scene.Links, geom.Link{ID: "lift", Revision: 1, Start: f.TraversalStart, End: f.TraversalEnd, Radius: 1, Bidirectional: true, Area: 1, Flags: 1, Action: "qualification-lift"})
		f.Anchors = append(f.Anchors, f.TraversalEnd)
		distant := c.HalfExtent * .5
		add(&f, quad("unaffected-island", distant-3, distant-3, distant+3, distant+3, 15, 15), "unaffected-traversal-island", geom.Identity())
		f.UnaffectedTraversalStart = geom.Vec3{X: distant - 6, Z: distant}
		f.UnaffectedTraversalEnd = geom.Vec3{X: distant, Y: 15, Z: distant}
		f.Scene.Links = append(f.Scene.Links, geom.Link{ID: "unaffected-lift", Revision: 1, Start: f.UnaffectedTraversalStart, End: f.UnaffectedTraversalEnd, Radius: 1, Bidirectional: true, Area: 1, Flags: 1, Action: "qualification-unaffected-lift"})
		f.Anchors = append(f.Anchors, f.UnaffectedTraversalStart, f.UnaffectedTraversalEnd)
		f.Features = append(f.Features, "distant traversal outside central edit quarantine")
	}
	if c.Family != "construction" && c.Family != "capacity" {
		repeatFeatures(&f, c)
	}
	if c.Family != "capacity" {
		add(&f, quad("removable-bridge", -8, 10.5, 8, 13.5, 7, 7), "bridge", geom.Identity())
		f.BridgeInstance = "bridge"
	}
	bounds := geom.Bounds{Min: geom.Vec3{X: -c.HalfExtent, Y: -8, Z: -c.HalfExtent}, Max: geom.Vec3{X: c.HalfExtent, Y: 24, Z: c.HalfExtent}}
	for _, p := range []geom.Profile{{ID: "human", Radius: .4, Height: 1.8, MaxSlope: 45, MaxClimb: .4}, {ID: "large", Radius: .8, Height: 2.4, MaxSlope: 40, MaxClimb: .3}} {
		f.BodyProfiles = append(f.BodyProfiles, p)
		cfg := bake.DefaultConfig("qualification-"+c.Family, bounds)
		cfg.Profile = p
		cfg.Profile.Radius += f.BakeClearanceMargin
		cfg.Settings.CellSize = c.CellSize
		cfg.Settings.CellHeight = c.CellHeight
		cfg.Settings.TileSize = int32(c.TileSize)
		cfg.Settings.BuildLayers = true
		if c.Family == "terrain" {
			cfg.Settings.MaxSimplificationError = .5
		}
		cfg.Strict = true
		if err := cfg.Validate(); err != nil {
			return f, err
		}
		f.Profiles = append(f.Profiles, cfg)
	}
	if _, err := f.Scene.Resolve(geom.Vec3{}, true); err != nil {
		return f, err
	}
	return f, nil
}

func repeatFeatures(f *Fixture, c Config) {
	instances := append([]geom.Instance(nil), f.Scene.Instances[1:]...)
	volumes := append([]geom.Volume(nil), f.Scene.Volumes...)
	for z := -c.HalfExtent + c.FeatureSpacing/2; z < c.HalfExtent-16; z += c.FeatureSpacing {
		for x := -c.HalfExtent + c.FeatureSpacing/2; x < c.HalfExtent-16; x += c.FeatureSpacing {
			if math.Abs(x) < 20 && math.Abs(z) < 20 {
				continue
			}
			suffix := fmt.Sprintf("@%.3f,%.3f", x, z)
			for _, original := range instances {
				i := original
				i.ID += suffix
				i.Transform[3] += x
				i.Transform[11] += z
				f.Scene.Instances = append(f.Scene.Instances, i)
			}
			for _, original := range volumes {
				v := original
				v.ID += suffix
				v.Bounds.Min.X += x
				v.Bounds.Max.X += x
				v.Bounds.Min.Z += z
				v.Bounds.Max.Z += z
				f.Scene.Volumes = append(f.Scene.Volumes, v)
			}
			if c.Family == "multilevel" {
				for i := 1; i <= 3; i++ {
					f.Anchors = append(f.Anchors, geom.Vec3{X: x, Y: float64(i) * 4, Z: z})
				}
			}
		}
	}
}

func height(c Config, x, z float64) float64 {
	if c.Family != "terrain" {
		return 0
	}
	phase := float64(c.Seed%1024) * .007
	return .75 * math.Sin(x/18+phase) * math.Cos(z/23+phase)
}
func add(f *Fixture, a geom.Asset, id string, t geom.Transform) {
	f.Scene.Assets = append(f.Scene.Assets, a)
	f.Scene.Instances = append(f.Scene.Instances, geom.Instance{ID: id, AssetID: a.ID, Revision: 1, Transform: t})
}
func box(id string, a, b geom.Vec3) geom.Volume {
	return geom.Volume{ID: id, Revision: 1, Shape: "box", Bounds: geom.Bounds{Min: a, Max: b}, Exclude: true}
}
func quad(id string, x0, z0, x1, z1, y0, y1 float64) geom.Asset {
	return geom.Asset{ID: id, Vertices: []geom.Vec3{{X: x0, Y: y0, Z: z0}, {X: x0, Y: y0, Z: z1}, {X: x1, Y: y1, Z: z1}, {X: x1, Y: y1, Z: z0}}, Triangles: []geom.Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}, {Indices: [3]uint32{0, 2, 3}, Area: 1}}}
}
func slab(id string, a, b geom.Vec3) geom.Asset {
	m := quad(id, a.X, a.Z, b.X, b.Z, b.Y, b.Y)
	m.Vertices = append(m.Vertices, geom.Vec3{X: a.X, Y: a.Y, Z: a.Z}, geom.Vec3{X: a.X, Y: a.Y, Z: b.Z}, geom.Vec3{X: b.X, Y: a.Y, Z: b.Z}, geom.Vec3{X: b.X, Y: a.Y, Z: a.Z})
	m.Triangles = append(m.Triangles, geom.Triangle{Indices: [3]uint32{4, 6, 5}, Area: 0}, geom.Triangle{Indices: [3]uint32{4, 7, 6}, Area: 0})
	return m
}
