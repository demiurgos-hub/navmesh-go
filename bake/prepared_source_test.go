package bake

import (
	"bytes"
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/recast"
)

func preparedFixture(degenerate bool) (geom.Scene, Config) {
	plane := func(id string, y, width float64) geom.Asset {
		return geom.Asset{ID: id, Vertices: []geom.Vec3{{X: -width, Y: y, Z: -width}, {X: -width, Y: y, Z: width}, {X: width, Y: y, Z: width}, {X: width, Y: y, Z: -width}}, Triangles: []geom.Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}, {Indices: [3]uint32{0, 2, 3}, Area: 1}}}
	}
	ground, bridge := plane("ground", 0, 8), plane("bridge", 3, 5)
	if degenerate {
		ground.Triangles = append(ground.Triangles, geom.Triangle{Indices: [3]uint32{0, 0, 0}, Area: 1})
	}
	mirror := geom.Identity()
	mirror[0] = -1
	scene := geom.Scene{Assets: []geom.Asset{ground, bridge}, Instances: []geom.Instance{{ID: "ground", AssetID: "ground", Transform: geom.Identity()}, {ID: "bridge", AssetID: "bridge", Transform: mirror}},
		Volumes: []geom.Volume{{ID: "paint", Shape: "convex", Bounds: geom.Bounds{Min: geom.Vec3{X: 1, Y: -.1, Z: 1}, Max: geom.Vec3{X: 3, Y: 1, Z: 3}}, Points: []geom.Vec3{{X: 1, Z: 1}, {X: 1, Z: 3}, {X: 3, Z: 3}, {X: 3, Z: 1}}, Area: 2}},
		Links:   []geom.Link{{ID: "stairs", Revision: 1, Start: geom.Vec3{X: -3, Y: .1}, End: geom.Vec3{X: -3, Y: 3.1}, Radius: .4, Flags: 1, Area: 3, Bidirectional: true, Profiles: []string{"human", "large"}}}}
	cfg := DefaultConfig("prepared", geom.Bounds{Min: geom.Vec3{X: -8, Y: -1, Z: -8}, Max: geom.Vec3{X: 8, Y: 8, Z: 8}})
	cfg.Settings.CellSize, cfg.Settings.TileSize = .25, 32
	return scene, cfg
}

// Reproduce the pre-capsule pipeline independently: resolve, index and build
// every requested column directly, without calling either prepared entry point.
func separateSourceBuild(t *testing.T, scene geom.Scene, cfg Config, keys []artifact.TileKey) *Result {
	t.Helper()
	resolved, err := scene.Resolve(cfg.Settings.Origin, cfg.Strict)
	if err != nil {
		t.Fatal(err)
	}
	index := geom.NewIndex(resolved.Triangles)
	links := append([]geom.Link(nil), scene.Links...)
	sortLinks(links)
	fingerprint, err := cfg.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Revision == 0 {
		cfg.Revision = 1
	}
	result := &Result{Bundle: artifact.Bundle{WorldID: cfg.WorldID, Revision: cfg.Revision, Profile: cfg.Profile, Settings: cfg.Settings, Links: links, SourceFingerprint: resolved.Fingerprint, ResolvedFingerprint: resolved.ResolvedFingerprint, SettingsFingerprint: fingerprint}, Sources: resolved.Diagnostics}
	if keys == nil {
		keys, err = TileKeys(cfg)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range keys {
		tile, diagnostic, err := buildTile(context.Background(), resolved, index, cfg, key, links)
		if err != nil {
			t.Fatal(err)
		}
		result.Bundle.Tiles = append(result.Bundle.Tiles, tile)
		result.Diagnostics = append(result.Diagnostics, diagnostic)
	}
	return result
}

func preparedBytes(t *testing.T, result *Result) []byte {
	t.Helper()
	data, err := artifact.Encode(result.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPreparedSourceMatchesIndependentBuild(t *testing.T) {
	for _, shifted := range []bool{false, true} {
		scene, cfg := preparedFixture(false)
		if shifted {
			offset := geom.Vec3{X: 1024, Y: 16, Z: -512}
			add := func(p geom.Vec3) geom.Vec3 { return geom.Vec3{X: p.X + offset.X, Y: p.Y + offset.Y, Z: p.Z + offset.Z} }
			for i := range scene.Instances {
				scene.Instances[i].Transform[3] += offset.X
				scene.Instances[i].Transform[7] += offset.Y
				scene.Instances[i].Transform[11] += offset.Z
			}
			for i := range scene.Volumes {
				v := &scene.Volumes[i]
				v.Bounds.Min, v.Bounds.Max = add(v.Bounds.Min), add(v.Bounds.Max)
				for j := range v.Points {
					v.Points[j] = add(v.Points[j])
				}
			}
			for i := range scene.Links {
				scene.Links[i].Start, scene.Links[i].End = add(scene.Links[i].Start), add(scene.Links[i].End)
			}
			cfg.Settings.Origin = offset
			cfg.Settings.Bounds.Min, cfg.Settings.Bounds.Max = add(cfg.Settings.Bounds.Min), add(cfg.Settings.Bounds.Max)
		}
		source, err := PrepareSource(scene, cfg.Settings.Origin, cfg.Strict)
		if err != nil {
			t.Fatal(err)
		}
		if source.data.index != nil {
			t.Fatal("source preparation eagerly built the index")
		}
		for _, mode := range []string{"layers", "monotone", "watershed"} {
			current := cfg
			current.Settings.Partition = mode
			current.Settings.BuildLayers = true
			if mode == "monotone" {
				current.Profile.ID, current.Profile.Radius, current.Profile.Height = "large", .6, 2.2
				current.AreaFlags[1] = 4
			}
			keys, err := TileKeys(current)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "watershed" {
				keys = keys[:2]
			}
			want := separateSourceBuild(t, scene, current, keys)
			got, err := source.BuildTiles(context.Background(), current, keys)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(preparedBytes(t, want), preparedBytes(t, got)) {
				t.Fatalf("canonical build changed: shifted=%v mode=%s", shifted, mode)
			}
			for i := range got.Diagnostics {
				got.Diagnostics[i].Duration = 0
			}
			if !reflect.DeepEqual(want.Diagnostics, got.Diagnostics) || !reflect.DeepEqual(want.Sources, got.Sources) {
				t.Fatal("deterministic bake diagnostics changed")
			}
		}
	}
}

func TestPreparedSourceOwnsInputsAndEveryResult(t *testing.T) {
	scene, cfg := preparedFixture(true)
	want := separateSourceBuild(t, scene, cfg, nil)
	expected := preparedBytes(t, want)
	source, err := PrepareSource(scene, cfg.Settings.Origin, cfg.Strict)
	if err != nil {
		t.Fatal(err)
	}
	if source.SourceFingerprint() != want.Bundle.SourceFingerprint || source.ResolvedFingerprint() != want.Bundle.ResolvedFingerprint {
		t.Fatal("source identities changed")
	}
	if source.TriangleCount() != 4 {
		t.Fatal("degenerate triangle was not omitted")
	}
	triangle, ok := source.TriangleAt(0)
	if !ok {
		t.Fatal("missing value triangle")
	}
	triangle.Vertices[0].X = math.NaN()
	triangle.Bounds.Min.Y = math.NaN()
	if again, _ := source.TriangleAt(0); !again.Vertices[0].Finite() || !again.Bounds.Valid() {
		t.Fatal("triangle accessor aliases source")
	}
	if _, ok := source.TriangleAt(-1); ok {
		t.Fatal("negative triangle index accepted")
	}
	if _, ok := source.TriangleAt(source.TriangleCount()); ok {
		t.Fatal("excess triangle index accepted")
	}
	scene.Assets[0].Vertices[0].X = math.NaN()
	scene.Assets[0].Triangles[0].Indices[0] = 99999
	scene.Instances[0].Transform[0] = math.NaN()
	scene.Volumes[0].Points[0].X = math.NaN()
	scene.Links[0].Profiles[0] = "caller-mutated"
	scene.Links[0].Start.Y = math.NaN()
	first, err := source.BuildTiles(context.Background(), cfg, nil)
	if err != nil || !bytes.Equal(expected, preparedBytes(t, first)) {
		t.Fatal("prepared source aliases caller", err)
	}
	if len(first.Sources) == 0 {
		t.Fatal("fixture lacks source diagnostic")
	}
	first.Sources[0].Message = "caller-mutated"
	first.Bundle.Links[0].Profiles[0] = "caller-mutated"
	first.Bundle.Links[0].End.Y = math.NaN()
	for i := range first.Bundle.Tiles {
		if len(first.Bundle.Tiles[i].Parameters.Verts) > 0 {
			first.Bundle.Tiles[i].Parameters.Verts[0]++
			break
		}
	}
	index := source.data.index
	copyOfHandle := *source
	second, err := copyOfHandle.BuildTiles(context.Background(), cfg, nil)
	if err != nil || !bytes.Equal(expected, preparedBytes(t, second)) || !reflect.DeepEqual(second.Sources, want.Sources) {
		t.Fatal("previous result aliases reusable source", err)
	}
	if index == nil || source.data.index != index || copyOfHandle.data.index != index {
		t.Fatal("immutable index was not reused")
	}
}

type preparedCancelContext struct {
	context.Context
	checks int
}

func (c *preparedCancelContext) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}
	return nil
}

func TestPreparedSourceRetainsValidationAndCancellation(t *testing.T) {
	scene, cfg := preparedFixture(false)
	source, err := PrepareSource(scene, cfg.Settings.Origin, cfg.Strict)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := TileKeys(cfg)
	for _, tc := range []struct {
		name string
		edit func(*Config)
		keys []artifact.TileKey
	}{
		{"origin", func(c *Config) { c.Settings.Origin.X++ }, nil},
		{"signed-zero-origin", func(c *Config) { c.Settings.Origin.X = math.Copysign(0, -1) }, nil},
		{"strict", func(c *Config) { c.Strict = true }, nil},
		{"invalid-profile", func(c *Config) { c.Profile.Height = -1 }, nil},
		{"tile-limit", func(c *Config) { c.MaxTiles = 1 }, nil},
		{"negative-limit", func(c *Config) { c.MaxTrianglesPerTile = -1 }, nil},
		{"duplicate-column", func(c *Config) {}, []artifact.TileKey{keys[0], keys[0]}},
		{"outside-column", func(c *Config) {}, []artifact.TileKey{{X: 9999}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			tc.edit(&c)
			if r, err := source.BuildTiles(context.Background(), c, tc.keys); err == nil || r != nil {
				t.Fatal("invalid build accepted")
			}
			if source.data.index != nil {
				t.Fatal("invalid request built an index")
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := source.BuildTiles(cancelled, cfg, nil); !errors.Is(err, context.Canceled) || r != nil || source.data.index != nil {
		t.Fatal("pre-cancelled build did work", err)
	}
	if r, err := source.BuildTiles(nil, cfg, nil); err == nil || r != nil {
		t.Fatal("nil context accepted")
	}
	if r, err := (*PreparedSource)(nil).BuildTiles(context.Background(), cfg, nil); err == nil || r != nil {
		t.Fatal("nil source accepted")
	}
	if r, err := new(PreparedSource).BuildTiles(context.Background(), cfg, nil); err == nil || r != nil {
		t.Fatal("zero source accepted")
	}
	if _, err := source.BuildTiles(context.Background(), cfg, []artifact.TileKey{}); err != nil || source.data.index != nil {
		t.Fatal("empty column request built an index", err)
	}
	if r, err := source.BuildTiles(&preparedCancelContext{Context: context.Background()}, cfg, nil); !errors.Is(err, context.Canceled) || r != nil || source.data.index == nil {
		t.Fatal("post-index cancellation was ignored", err)
	}
	limited := cfg
	limited.MaxTrianglesPerTile = 1
	if r, err := source.BuildTiles(context.Background(), limited, nil); err == nil || r != nil {
		t.Fatal("cached source bypassed triangle limit")
	}
	limited = cfg
	limited.MaxDetailSamplesPerTile = 1
	if r, err := source.BuildTiles(context.Background(), limited, nil); !errors.Is(err, recast.ErrCapacity) || r != nil {
		t.Fatal("cached source bypassed detail limit", err)
	}
	if r, err := source.BuildTiles(context.Background(), cfg, nil); err != nil || r == nil {
		t.Fatal("failed/cancelled build poisoned reusable source", err)
	}
	degenerate, _ := preparedFixture(true)
	if _, err := PrepareSource(degenerate, geom.Vec3{}, true); err == nil {
		t.Fatal("strict preparation accepted degenerate input")
	}
}

func TestPreparedSourceConcurrentBuildsOwnResults(t *testing.T) {
	scene, cfg := preparedFixture(false)
	expected := preparedBytes(t, separateSourceBuild(t, scene, cfg, nil))
	source, err := PrepareSource(scene, cfg.Settings.Origin, cfg.Strict)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	errors := make(chan error, workers)
	results := make(chan *Result, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := source.BuildTiles(context.Background(), cfg, nil)
			if err != nil {
				errors <- err
				return
			}
			results <- r
		}()
	}
	wg.Wait()
	close(errors)
	close(results)
	for err := range errors {
		t.Fatal(err)
	}
	count := 0
	for r := range results {
		count++
		if !bytes.Equal(expected, preparedBytes(t, r)) {
			t.Fatal("concurrent canonical result changed")
		}
		r.Bundle.Links[0].Profiles[0] = "caller-mutated"
	}
	if count != workers {
		t.Fatal("missing concurrent build result")
	}
	last, err := source.BuildTiles(context.Background(), cfg, nil)
	if err != nil || !bytes.Equal(expected, preparedBytes(t, last)) {
		t.Fatal("concurrent results mutated prepared source", err)
	}
}
