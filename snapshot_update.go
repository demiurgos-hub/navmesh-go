package navmesh

import (
	"errors"
	"math"
	"sort"

	"github.com/demiurgos-hub/navmesh-go/artifact"
)

// RebuildColumns builds an independent runtime snapshot from updated metadata
// and replacement tile columns. update must keep this snapshot's world, profile
// and settings, supply the complete current link metadata, and contain tiles only
// in replaceColumns. Column keys have layer zero; replacement removes every old
// layer before inserting update's tiles. unavailable removes complete columns,
// including replacements. An explicit empty tile represents loaded empty space.
// As with NewSnapshot, the caller supplies snapshot and tile revision identities;
// advance them when geometry changes so retained dependency checks remain valid.
//
// Caller-owned update data is copied. Unchanged construction arrays may be shared
// privately as immutable data, but runtime tile bytes, adjacency and the snapshot
// itself are never shared or retained. The complete merged bundle receives the
// same validation and default portable limits as NewSnapshot. Failure leaves
// this snapshot and caller inputs unchanged. Retain the base lease for this call.
func (s *Snapshot) RebuildColumns(update artifact.Bundle, replaceColumns, unavailable []artifact.TileKey) (*Snapshot, error) {
	if s == nil {
		return nil, errors.New("navmesh: nil base snapshot")
	}
	base := s.bundle
	if update.WorldID != base.WorldID || update.Profile != base.Profile || update.Settings != base.Settings || update.SettingsFingerprint != base.SettingsFingerprint {
		return nil, errors.New("navmesh: incompatible construction update")
	}
	replace, err := s.columnSet(replaceColumns)
	if err != nil {
		return nil, err
	}
	unloaded, err := s.columnSet(unavailable)
	if err != nil {
		return nil, err
	}
	for _, tile := range update.Tiles {
		if !replace[artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}] {
			return nil, errors.New("navmesh: replacement tile outside selected columns")
		}
	}
	owned, err := update.Clone()
	if err != nil {
		return nil, err
	}
	count := 0
	for _, tile := range base.Tiles {
		key := artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}
		if !replace[key] && !unloaded[key] {
			count++
		}
	}
	for _, tile := range owned.Tiles {
		if !unloaded[artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}] {
			count++
		}
	}
	if uint64(count) > artifact.DefaultLimits().MaxTiles {
		return nil, errors.New("navmesh: merged tile limit exceeded")
	}
	tiles := make([]artifact.Tile, 0, count)
	for _, tile := range base.Tiles {
		key := artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}
		if !replace[key] && !unloaded[key] {
			tiles = append(tiles, tile)
		}
	}
	for _, tile := range owned.Tiles {
		if !unloaded[artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}] {
			tiles = append(tiles, tile)
		}
	}
	sort.Slice(tiles, func(i, j int) bool {
		a, b := tiles[i].Key, tiles[j].Key
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		return a.Layer < b.Layer
	})
	owned.Tiles = tiles
	if err := owned.ValidatePortable(); err != nil {
		return nil, err
	}
	return assembleSnapshot(owned)
}

func (s *Snapshot) columnSet(keys []artifact.TileKey) (map[artifact.TileKey]bool, error) {
	if uint64(len(keys)) > artifact.DefaultLimits().MaxTiles {
		return nil, errors.New("navmesh: too many update columns")
	}
	settings := s.bundle.Settings
	width := settings.CellSize * float64(settings.TileSize)
	x0 := math.Floor((settings.Bounds.Min.X - settings.Origin.X) / width)
	x1 := math.Ceil((settings.Bounds.Max.X - settings.Origin.X) / width)
	z0 := math.Floor((settings.Bounds.Min.Z - settings.Origin.Z) / width)
	z1 := math.Ceil((settings.Bounds.Max.Z - settings.Origin.Z) / width)
	set := make(map[artifact.TileKey]bool, len(keys))
	for _, key := range keys {
		if key.Layer != 0 || float64(key.X) < x0 || float64(key.X) >= x1 || float64(key.Z) < z0 || float64(key.Z) >= z1 || set[key] {
			return nil, errors.New("navmesh: duplicate or invalid update column")
		}
		set[key] = true
	}
	return set, nil
}
