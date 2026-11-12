package navmesh

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func columnUpdateFixture(t *testing.T) (*Snapshot, artifact.Bundle, artifact.TileKey) {
	t.Helper()
	scene, cfg := testworld.Scene()
	cfg.Settings.BuildLayers = true
	scene.Links = []geom.Link{{ID: "stairs", Start: Vec3{X: -4, Y: .1}, End: Vec3{X: -4, Y: 3.1}, Radius: .4, Flags: 1, Profiles: []string{"z", "human"}}}
	r, err := bake.Build(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	base, err := NewSnapshot(r.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, tile := range r.Bundle.Tiles {
		if tile.Parameters.OffMeshConCount > 0 {
			update := r.Bundle
			update.Revision++
			tile.Revision++
			update.Tiles = []artifact.Tile{tile}
			return base, update, artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}
		}
	}
	t.Fatal("fixture has no link-bearing tile")
	return nil, artifact.Bundle{}, artifact.TileKey{}
}

func snapshotBytes(t *testing.T, s *Snapshot) []byte {
	t.Helper()
	b, err := s.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	data, err := artifact.Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRebuildColumnsCanonicalOwnershipAndIndependentRuntime(t *testing.T) {
	base, update, key := columnUpdateFixture(t)
	before := snapshotBytes(t, base)
	full, err := base.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	tiles := full.Tiles[:0]
	for _, tile := range full.Tiles {
		if tile.Key.X != key.X || tile.Key.Z != key.Z {
			tiles = append(tiles, tile)
		}
	}
	full = update
	full.Tiles = append(tiles, update.Tiles...)
	baseline, err := NewSnapshot(full)
	if err != nil {
		t.Fatal(err)
	}
	next, err := base.RebuildColumns(update, []artifact.TileKey{key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := snapshotBytes(t, baseline)
	if !bytes.Equal(want, snapshotBytes(t, next)) {
		t.Fatal("shared construction differs from full-copy canonical baseline")
	}
	shared := 0
	for _, tile := range next.bundle.Tiles {
		if tile.Empty || tile.Key.X == key.X && tile.Key.Z == key.Z {
			continue
		}
		for _, old := range base.bundle.Tiles {
			if tile.Key != old.Key {
				continue
			}
			if &tile.Parameters.Verts[0] != &old.Parameters.Verts[0] {
				t.Fatal("unchanged construction unnecessarily copied")
			}
			for j := range tile.Layers {
				if len(tile.Layers[j].Heights) > 0 && &tile.Layers[j].Heights[0] != &old.Layers[j].Heights[0] {
					t.Fatal("unchanged heightfield layer unnecessarily copied")
				}
			}
			shared++
		}
	}
	if shared == 0 || next.mesh == base.mesh || next.identity == base.identity {
		t.Fatal("fixture did not share construction with independent runtime identity")
	}
	for i := 0; i < int(next.mesh.GetMaxTiles()); i++ {
		tile := next.mesh.GetTile(i)
		if len(tile.Data) == 0 {
			continue
		}
		for j := 0; j < int(base.mesh.GetMaxTiles()); j++ {
			old := base.mesh.GetTile(j)
			if len(old.Data) > 0 && &tile.Data[0] == &old.Data[0] {
				t.Fatal("runtime tile payload is shared")
			}
		}
	}
	mutateUpdateSlices(reflect.ValueOf(&update).Elem(), false)
	exported, err := next.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	mutateUpdateSlices(reflect.ValueOf(&exported).Elem(), false)
	if !bytes.Equal(want, snapshotBytes(t, next)) || !bytes.Equal(before, snapshotBytes(t, base)) {
		t.Fatal("caller or exported nested values alias immutable construction")
	}
}

func mutateUpdateSlices(v reflect.Value, owned bool) {
	switch v.Kind() {
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutateUpdateSlices(v.Index(i), true)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			mutateUpdateSlices(v.Field(i), owned)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			mutateUpdateSlices(v.Index(i), owned)
		}
	default:
		if owned && v.CanSet() {
			v.Set(reflect.Zero(v.Type()))
		}
	}
}

func TestRebuildColumnsRejectsIncompatibleOrInvalidMergedData(t *testing.T) {
	base, update, key := columnUpdateFixture(t)
	before := snapshotBytes(t, base)
	cases := map[string]func(*artifact.Bundle){
		"world":               func(b *artifact.Bundle) { b.WorldID += "other" },
		"profile":             func(b *artifact.Bundle) { b.Profile.ID += "other" },
		"settings":            func(b *artifact.Bundle) { b.Settings.Origin.X++ },
		"fingerprint":         func(b *artifact.Bundle) { b.SourceFingerprint = "invalid" },
		"settingsFingerprint": func(b *artifact.Bundle) { b.SettingsFingerprint = strings.Repeat("0", 64) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := update.Clone()
			if err != nil {
				t.Fatal(err)
			}
			change(&b)
			if _, err := base.RebuildColumns(b, []artifact.TileKey{key}, nil); err == nil {
				t.Fatal("invalid update accepted")
			}
		})
	}
	metadata := update
	metadata.Tiles = nil
	metadata.Links = nil
	if err := metadata.ValidatePortable(); err != nil {
		t.Fatal("delta alone should be valid", err)
	}
	if _, err := base.RebuildColumns(metadata, nil, nil); err == nil {
		t.Fatal("merged untouched off-mesh tile lost its metadata without rejection")
	}
	for _, columns := range [][]artifact.TileKey{nil, {key, key}, {{X: key.X, Z: key.Z, Layer: 1}}, {{X: 999}}} {
		if _, err := base.RebuildColumns(update, columns, nil); err == nil {
			t.Fatal("invalid replacement selection accepted", columns)
		}
	}
	if !bytes.Equal(before, snapshotBytes(t, base)) {
		t.Fatal("failed update changed base")
	}
}

func TestRebuildColumnsReplacesAndUnloadsEveryLayer(t *testing.T) {
	base, update, key := columnUpdateFixture(t)
	bundle, err := base.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	// A second loaded layer exercises full-column replacement independently of
	// the current baker's choice to emit one runtime tile per column.
	extra := update.Tiles[0]
	extra.Key.Layer, extra.Parameters.TileLayer = 1, 1
	extra.Parameters.OffMeshConCount = 0
	extra.Parameters.OffMeshConVerts = nil
	extra.Parameters.OffMeshConRad = nil
	extra.Parameters.OffMeshConFlags = nil
	extra.Parameters.OffMeshConAreas = nil
	extra.Parameters.OffMeshConDir = nil
	extra.Parameters.OffMeshConUserID = nil
	bundle.Tiles = append(bundle.Tiles, extra)
	base, err = NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	next, err := base.RebuildColumns(update, []artifact.TileKey{key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.tiles[artifact.TileKey{X: key.X, Z: key.Z, Layer: 1}]; ok {
		t.Fatal("old upper layer survived full-column replacement")
	}
	metadata := update
	metadata.Tiles = nil
	metadata.Revision++
	unloaded, err := base.RebuildColumns(metadata, nil, []artifact.TileKey{key})
	if err != nil {
		t.Fatal(err)
	}
	for tile := range unloaded.tiles {
		if tile.X == key.X && tile.Z == key.Z {
			t.Fatal("unloaded column retained a layer")
		}
	}
	if !unloaded.incomplete || len(base.tiles) != len(next.tiles)+1 {
		t.Fatal("streaming availability or old layer identity lost")
	}
	metadata.Revision++
	if _, err := unloaded.RebuildColumns(metadata, nil, nil); err != nil {
		t.Fatal("metadata-only empty replacement failed", err)
	}
	update.Revision = metadata.Revision + 1
	reloaded, err := unloaded.RebuildColumns(update, []artifact.TileKey{key}, nil)
	if err != nil || reloaded.incomplete {
		t.Fatal("reload did not restore column", err)
	}
}

func TestRebuildColumnsChecksMergedPortableElementLimit(t *testing.T) {
	cfg := bake.DefaultConfig("limits", geom.Bounds{Min: Vec3{Y: -1}, Max: Vec3{X: 102, Y: 8, Z: 51}})
	cfg.Settings.TileSize = 255
	fingerprint, err := cfg.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	layer := artifact.Layer{BoundsMin: [3]float32{0, -1, 0}, BoundsMax: [3]float32{51, 8, 51}, CellSize: .2, CellHeight: .1, Width: 255, Height: 255, MaxX: 254, MaxZ: 254, HMax: 1,
		Heights: make([]uint8, 255*255), Areas: make([]uint8, 255*255), Connections: make([]uint8, 255*255)}
	tile := artifact.Tile{Revision: 1, Empty: true, Parameters: detour.DtNavMeshCreateParams{}, Layers: make([]artifact.Layer, 85)}
	for i := range tile.Layers {
		tile.Layers[i] = layer
	}
	b := artifact.Bundle{WorldID: cfg.WorldID, Revision: 1, Profile: cfg.Profile, Settings: cfg.Settings, SourceFingerprint: fingerprint, ResolvedFingerprint: fingerprint, SettingsFingerprint: fingerprint, Tiles: []artifact.Tile{tile}}
	base, err := NewSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	layer.BoundsMin[0], layer.BoundsMax[0] = 51, 102
	b.Revision++
	b.Tiles = []artifact.Tile{{Key: artifact.TileKey{X: 1}, Revision: 2, Empty: true, Layers: []artifact.Layer{layer, layer}}}
	if err := b.ValidatePortable(); err != nil {
		t.Fatal("replacement alone exceeds limit", err)
	}
	if _, err := base.RebuildColumns(b, []artifact.TileKey{{X: 1}}, nil); err == nil || !strings.Contains(err.Error(), "collection limit") {
		t.Fatal("merged aggregate element limit not enforced", err)
	}
}
