package worldgen

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/bake"
)

func TestCapacityCorridorHasLongReachableRoutesBothDirections(t *testing.T) {
	c := DefaultConfig()
	c.Family = "capacity"
	f, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Anchors) != 2 || len(f.Scene.Links) != 0 || f.BridgeInstance != "" {
		t.Fatal("capacity must have two stable endpoints and no traversal/dynamic bridge")
	}
	for _, cfg := range f.Profiles {
		built, err := bake.Build(context.Background(), f.Scene, cfg)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := navmesh.NewSnapshot(built.Bundle)
		if err != nil {
			t.Fatal(err)
		}
		q, err := snapshot.NewQuery(navmesh.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		var endpoints [2]navmesh.Surface
		for i, anchor := range f.Anchors {
			hit := q.Project(context.Background(), anchor, navmesh.ProjectionOptions{Horizontal: 1, Up: 1, Down: 1}, navmesh.Filter{})
			if hit.Code != navmesh.Complete {
				t.Fatalf("%s endpoint%d: %+v", cfg.Profile.ID, i, hit.Status)
			}
			endpoints[i] = hit.Surface
		}
		for direction := 0; direction < 2; direction++ {
			start, end := endpoints[direction], endpoints[1-direction]
			path := q.FindPath(context.Background(), start, end, navmesh.Filter{})
			if path.Code != navmesh.Complete || path.Path == nil || path.Path.End().Position() != end.Position() {
				t.Fatalf("%s direction%d: %+v", cfg.Profile.ID, direction, path.Status)
			}
			length, last := 0.0, start.Position()
			for _, corner := range path.Path.Corners() {
				dx, dy, dz := corner.Position.X-last.X, corner.Position.Y-last.Y, corner.Position.Z-last.Z
				length += math.Sqrt(dx*dx + dy*dy + dz*dz)
				last = corner.Position
			}
			if length < 3268 {
				t.Fatalf("%s direction%d length%gm cannot sustain4m/s for12min with10%%+100m margin", cfg.Profile.ID, direction, length)
			}
			t.Logf("%s direction%d route%gm", cfg.Profile.ID, direction, length)
		}
	}
}

func TestFixtureFamiliesBakeBothClearances(t *testing.T) {
	for _, family := range []string{"terrain", "multilevel", "construction"} {
		t.Run(family, func(t *testing.T) {
			c := DefaultConfig()
			c.Family = family
			c.HalfExtent = 16
			c.GridStep = 2
			c.TileSize = 32
			f, err := Generate(c)
			if err != nil {
				t.Fatal(err)
			}
			again, err := Generate(c)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(f)
			b, _ := json.Marshal(again)
			if string(a) != string(b) {
				t.Fatal("non-deterministic fixture")
			}
			for _, cfg := range f.Profiles {
				built, err := bake.Build(context.Background(), f.Scene, cfg)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := navmesh.NewSnapshot(built.Bundle)
				if err != nil {
					t.Fatal(err)
				}
				q, err := snapshot.NewQuery(navmesh.Limits{})
				if err != nil {
					t.Fatal(err)
				}
				valid := 0
				for _, p := range f.Anchors {
					r := q.Project(context.Background(), p, navmesh.ProjectionOptions{Horizontal: 1, Up: 1, Down: 1}, navmesh.Filter{})
					if r.Code == navmesh.Complete {
						valid++
					}
				}
				if valid < 20 {
					t.Fatalf("%s has only %d valid anchors", cfg.Profile.ID, valid)
				}
				if family == "multilevel" {
					// Projectable stacked floors can still be disconnected when a
					// ramp runs beneath an uncut floor without sufficient headroom.
					ground := q.Project(context.Background(), navmesh.Vec3{}, navmesh.ProjectionOptions{Horizontal: 1, Up: 1, Down: 1}, navmesh.Filter{})
					if ground.Code != navmesh.Complete {
						t.Fatalf("%s ground projection: %+v", cfg.Profile.ID, ground.Status)
					}
					previous := ground.Surface
					for floor := 1; floor <= 3; floor++ {
						hit := q.Project(context.Background(), navmesh.Vec3{Y: float64(floor) * 4}, navmesh.ProjectionOptions{Horizontal: 1, Up: 1, Down: 1}, navmesh.Filter{})
						if hit.Code != navmesh.Complete {
							t.Fatalf("%s floor%d projection: %+v", cfg.Profile.ID, floor, hit.Status)
						}
						for _, endpoints := range [][2]navmesh.Surface{{ground.Surface, hit.Surface}, {hit.Surface, ground.Surface}, {previous, hit.Surface}, {hit.Surface, previous}} {
							path := q.FindPath(context.Background(), endpoints[0], endpoints[1], navmesh.Filter{})
							if path.Code != navmesh.Complete || path.Path == nil || path.Path.End().Position() != endpoints[1].Position() {
								t.Fatalf("%s floor%d route %v -> %v: %+v", cfg.Profile.ID, floor, endpoints[0].Position(), endpoints[1].Position(), path.Status)
							}
						}
						previous = hit.Surface
					}
				}
			}
		})
	}
}

func TestRejectUnboundedGenerator(t *testing.T) {
	c := DefaultConfig()
	c.GridStep = .001
	if _, err := Generate(c); err == nil {
		t.Fatal("expected excessive allocation rejection")
	}
	c = DefaultConfig()
	c.Family = "unknown"
	if _, err := Generate(c); err == nil {
		t.Fatal("expected unsupported family rejection")
	}
}
