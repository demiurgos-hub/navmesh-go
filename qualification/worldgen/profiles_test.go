package worldgen

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func TestBodyProfilesAndCapacityBakeMargin(t *testing.T) {
	bodies := []geom.Profile{
		{ID: "human", Radius: .4, Height: 1.8, MaxSlope: 45, MaxClimb: .4},
		{ID: "large", Radius: .8, Height: 2.4, MaxSlope: 40, MaxClimb: .3},
	}
	for _, cellSize := range []float64{.2, .25, .5} {
		c := DefaultConfig()
		c.Family, c.HalfExtent, c.CellSize = "capacity", 64, cellSize
		f, err := Generate(c)
		if err != nil {
			t.Fatal(err)
		}
		if f.Version != 1 || !reflect.DeepEqual(f.BodyProfiles, bodies) || len(f.Profiles) != len(bodies) || f.BakeClearanceMargin != 2*cellSize {
			t.Fatalf("cell size %g: body/margin metadata changed: %+v margin %g", cellSize, f.BodyProfiles, f.BakeClearanceMargin)
		}
		for i, cfg := range f.Profiles {
			want := bodies[i]
			want.Radius += 2 * cellSize
			if cfg.Profile != want || cfg.Settings.CellSize != cellSize || cfg.Settings.MaxSimplificationError != 1.3 {
				t.Fatalf("cell size %g: baked profile %+v, want %+v", cellSize, cfg.Profile, want)
			}
		}
		// Metadata is independently owned; changing the body description must
		// not change the bake configuration used to produce an artifact.
		before := f.Profiles[0].Profile
		f.BodyProfiles[0].Radius = 99
		if f.Profiles[0].Profile != before {
			t.Fatal("body metadata aliases the bake profile")
		}
	}
}

func TestMixedFamilyBakePresetsAndPhysicalProfiles(t *testing.T) {
	bodies := []geom.Profile{
		{ID: "human", Radius: .4, Height: 1.8, MaxSlope: 45, MaxClimb: .4},
		{ID: "large", Radius: .8, Height: 2.4, MaxSlope: 40, MaxClimb: .3},
	}
	for _, preset := range []struct {
		family       string
		contourError float64
	}{{"terrain", .5}, {"multilevel", 1.3}, {"construction", 1.3}} {
		t.Run(preset.family, func(t *testing.T) {
			family := preset.family
			c := DefaultConfig()
			c.Family, c.HalfExtent, c.GridStep, c.TileSize = family, 16, 2, 32
			f, err := Generate(c)
			if err != nil {
				t.Fatal(err)
			}
			if f.BakeClearanceMargin != 0 || !reflect.DeepEqual(f.BodyProfiles, bodies) || len(f.Profiles) != len(bodies) {
				t.Fatalf("mixed workload physical metadata changed: %+v margin %g", f.BodyProfiles, f.BakeClearanceMargin)
			}
			for i, cfg := range f.Profiles {
				want := bake.Config{
					WorldID: "qualification-" + family, Revision: 1, Profile: bodies[i], Strict: true,
					MaxTiles: 65536, MaxTrianglesPerTile: 1 << 20,
					Settings: artifact.Settings{
						Bounds:   geom.Bounds{Min: geom.Vec3{X: -16, Y: -8, Z: -16}, Max: geom.Vec3{X: 16, Y: 24, Z: 16}},
						CellSize: .25, CellHeight: .1, TileSize: 32, Partition: "layers", BuildLayers: true,
						MergeRegionArea: 400, MaxEdgeLength: 60, MaxSimplificationError: preset.contourError,
						DetailSampleDistance: 1.2, DetailSampleMaxError: .1,
					},
				}
				if cfg != want {
					t.Fatalf("%s bake config changed:\n got %+v\nwant %+v", bodies[i].ID, cfg, want)
				}
			}
		})
	}
}

func TestVersionOneFixtureOptionalBodyMetadata(t *testing.T) {
	legacy := []byte(`{"Version":1,"Profiles":[{"Profile":{"ID":"human","Radius":0.4,"Height":1.8,"MaxSlope":45,"MaxClimb":0.4}}]}`)
	var f Fixture
	if err := json.Unmarshal(legacy, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || len(f.BodyProfiles) != 0 || f.BakeClearanceMargin != 0 || len(f.Profiles) != 1 || f.Profiles[0].Profile.Radius != .4 {
		t.Fatal("legacy version 1 fixture requires new metadata")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["BodyProfiles"]; exists {
		t.Fatal("absent body metadata was written")
	}
	if _, exists := fields["BakeClearanceMargin"]; exists {
		t.Fatal("zero bake margin was written")
	}
}
