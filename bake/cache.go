package bake

import (
	"fmt"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/loader"
	dtcache "github.com/demiurgos-hub/navmesh-go/tilecache"
)

// CompressedLayer is an independently owned FastLZ tile-cache layer. Layer IDs
// are revision-local; callers must replace the complete column set together.
type CompressedLayer struct {
	Key      artifact.TileKey
	Revision uint64
	Data     []byte
}

// CompressLayers produces checked compressed cache data from pristine baked
// layers. This does not certify a blocker fast path: callers must separately
// qualify clearance expansion and height fidelity. Full source rebuilding is
// authoritative when those contracts cannot be met.
func CompressLayers(bundle artifact.Bundle) ([]CompressedLayer, error) {
	if err := bundle.Validate(); err != nil {
		return nil, err
	}
	if !bundle.Settings.BuildLayers {
		return nil, fmt.Errorf("bake: layer production was not enabled")
	}
	var result []CompressedLayer
	compressor := &loader.FastLZCompressor{}
	for _, tile := range bundle.Tiles {
		for i, layer := range tile.Layers {
			h := dtcache.DtTileCacheLayerHeader{Magic: dtcache.DT_TILECACHE_MAGIC, Version: dtcache.DT_TILECACHE_VERSION, Tx: tile.Key.X, Ty: tile.Key.Z, Tlayer: int32(i), Bmin: layer.BoundsMin, Bmax: layer.BoundsMax, Hmin: uint16(layer.HMin), Hmax: uint16(layer.HMax), Width: uint8(layer.Width), Height: uint8(layer.Height), Minx: uint8(layer.MinX), Maxx: uint8(layer.MaxX), Miny: uint8(layer.MinZ), Maxy: uint8(layer.MaxZ)}
			var data []byte
			var size int32
			status := dtcache.DtBuildTileCacheLayer(compressor, &h, layer.Heights, layer.Areas, layer.Connections, &data, &size)
			if detour.DtStatusFailed(status) {
				return nil, fmt.Errorf("bake: compress column %v layer %d: %#x", tile.Key, i, status)
			}
			result = append(result, CompressedLayer{artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z, Layer: int32(i)}, tile.Revision, data})
		}
	}
	return result, nil
}
