package bake_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
	"github.com/demiurgos-hub/navmesh-go/recast"
)

func TestBakeArtifactRuntime(t *testing.T) {
	for _, mode := range []string{"monotone", "watershed", "layers"} {
		t.Run(mode, func(t *testing.T) {
			scene, c := testworld.Scene()
			c.Settings.Partition = mode
			c.Settings.BuildLayers = true
			r, err := bake.Build(context.Background(), scene, c)
			if err != nil {
				t.Fatal(err)
			}
			a, err := artifact.Encode(r.Bundle)
			if err != nil {
				t.Fatal(err)
			}
			b, err := artifact.Decode(a, artifact.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			again, err := artifact.Encode(b)
			if err != nil || !bytes.Equal(a, again) {
				t.Fatal("noncanonical roundtrip", err)
			}
			snapshot, err := navmesh.NewSnapshot(b)
			if err != nil {
				t.Fatal(err)
			}
			q, err := snapshot.NewQuery(navmesh.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			project := func(p geom.Vec3) navmesh.Surface {
				r := q.Project(context.Background(), p, navmesh.ProjectionOptions{Horizontal: .5, Up: .4, Down: .4}, navmesh.Filter{})
				if r.Code != navmesh.Complete {
					t.Fatalf("project %+v: %+v", p, r.Status)
				}
				return r.Surface
			}
			ground := project(geom.Vec3{X: -5, Y: .1, Z: 0})
			upper := project(geom.Vec3{X: -5, Y: 3.1, Z: 0})
			if math.Abs(ground.Position().Y-upper.Position().Y) < 2 {
				t.Fatal("stacked floors collapsed")
			}
			end := project(geom.Vec3{X: 5, Y: 3.1, Z: 0})
			path := q.FindPath(context.Background(), upper, end, navmesh.Filter{})
			if path.Code != navmesh.Complete || len(path.Path.Corners()) < 2 {
				t.Fatalf("cross-tile bridge path: %+v", path.Status)
			}
			different := q.FindPath(context.Background(), ground, end, navmesh.Filter{})
			if different.Code == navmesh.Complete {
				t.Fatal("disconnected floors linked")
			}
			step := q.Move(context.Background(), upper, geom.Vec3{X: -4, Y: 3, Z: 0}, navmesh.Filter{})
			if step.Code != navmesh.Complete || math.Abs(step.Surface.Position().Y-upper.Position().Y) > .2 {
				t.Fatalf("move %+v", step)
			}
			cancel, cancelFn := context.WithCancel(context.Background())
			cancelFn()
			if q.FindPath(cancel, upper, end, navmesh.Filter{}).Code != navmesh.Cancelled {
				t.Fatal("cancel ignored")
			}
		})
	}
}
func TestEmptyAndInvalid(t *testing.T) {
	_, c := testworld.Scene()
	r, err := bake.Build(context.Background(), geom.Scene{}, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, tile := range r.Bundle.Tiles {
		if !tile.Empty {
			t.Fatal("empty source not explicit")
		}
	}
	c.Settings.CellSize = math.NaN()
	if _, err = bake.Build(context.Background(), geom.Scene{}, c); err == nil {
		t.Fatal("accepted NaN")
	}
}

func TestTileKeysRejectInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*bake.Config)
	}{
		{"nan cell size", func(c *bake.Config) { c.Settings.CellSize = math.NaN() }},
		{"infinite cell size", func(c *bake.Config) { c.Settings.CellSize = math.Inf(1) }},
		{"nan origin", func(c *bake.Config) { c.Settings.Origin.X = math.NaN() }},
		{"infinite origin", func(c *bake.Config) { c.Settings.Origin.Z = math.Inf(-1) }},
		{"zero tile size", func(c *bake.Config) { c.Settings.TileSize = 0 }},
		{"negative limit", func(c *bake.Config) { c.MaxTiles = -1 }},
		{"negative detail limit", func(c *bake.Config) { c.MaxDetailSamplesPerTile = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, c := testworld.Scene()
			tc.edit(&c)
			if keys, err := bake.TileKeys(c); err == nil || keys != nil {
				t.Fatalf("invalid config returned keys=%v, err=%v", keys, err)
			}
		})
	}
}

func TestDetailSamplingCapacity(t *testing.T) {
	scene, c := testworld.Scene()
	c.MaxDetailSamplesPerTile = 1
	result, err := bake.Build(context.Background(), scene, c)
	if result != nil || !errors.Is(err, recast.ErrCapacity) {
		t.Fatalf("detail sample budget returned result=%v, err=%v", result != nil, err)
	}
}

func BenchmarkBake(b *testing.B) {
	scene, c := testworld.Scene()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := bake.Build(context.Background(), scene, c); err != nil {
			b.Fatal(err)
		}
	}
}
