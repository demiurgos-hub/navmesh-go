package bake_test

import (
	"context"
	"math"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func TestBinary32ClearanceAgreesAcrossPaddingBakeAndArtifactValidation(t *testing.T) {
	c := bake.DefaultConfig("precision", geom.Bounds{Min: geom.Vec3{X: -4, Y: -1, Z: -4}, Max: geom.Vec3{X: 4, Y: 4, Z: 4}})
	c.Settings.CellSize, c.Settings.CellHeight = float64(float32(.1)), float64(float32(.1))
	c.Profile.Height, c.Profile.Radius, c.Profile.MaxClimb = float64(float32(.7)), float64(float32(.7)), .5
	c.Settings.ClearancePrecision = artifact.ClearanceBinary32
	c.Settings.TileSize = 32
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	clearance, err := c.Settings.QuantizeClearance(c.Profile)
	if err != nil {
		t.Fatal(err)
	}
	key := artifact.TileKey{}
	box := bake.TileBounds(c, key)
	if box.Min.X != -float64(clearance.Radius+3)*c.Settings.CellSize {
		t.Fatal("tile padding differs from selected clearance")
	}
	scene := geom.Scene{Assets: []geom.Asset{{ID: "floor", Vertices: []geom.Vec3{{X: -4, Z: -4}, {X: -4, Z: 4}, {X: 4, Z: 4}, {X: 4, Z: -4}}, Triangles: []geom.Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}, {Indices: [3]uint32{0, 2, 3}, Area: 1}}}}, Instances: []geom.Instance{{ID: "floor", AssetID: "floor", Transform: geom.Identity()}}}
	r, err := bake.Build(context.Background(), scene, c)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tile := range r.Bundle.Tiles {
		if !tile.Empty {
			count++
		}
	}
	if count == 0 {
		t.Fatal("test did not bake physical polygons")
	}
	data, err := artifact.Encode(r.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := artifact.Decode(data, artifact.DefaultLimits())
	if err != nil || decoded.Settings != c.Settings {
		t.Fatalf("artifact changed policy or rejected matching clearance: %v", err)
	}
	c.Settings.ClearancePrecision = "invalid"
	if err := c.Validate(); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, err := c.Fingerprint(); err == nil {
		t.Fatal("invalid mode fingerprinted")
	}
	if bounds := bake.TileBounds(c, key); bounds.Valid() || !math.IsNaN(bounds.Min.X) {
		t.Fatal("invalid mode produced valid tile bounds")
	}
}
