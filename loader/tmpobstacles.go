package loader

import (
	"bytes"
	"encoding/binary"
	"fmt"

	detour "github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/fastlz"
	dtcache "github.com/demiurgos-hub/navmesh-go/tilecache"
)

const TILECACHESET_MAGIC = 'T'<<24 | 'S'<<16 | 'E'<<8 | 'T' //'TSET';
const TILECACHESET_VERSION = 1

type TileCacheSetHeader struct {
	magic       int32
	version     int32
	numTiles    int32
	params      detour.DtNavMeshParams
	cacheParams dtcache.DtTileCacheParams
}

type TileCacheTileHeader struct {
	tileRef  dtcache.DtCompressedTileRef
	dataSize int32
}

type FastLZCompressor struct{}

// MaxCompressedSize returns a safe FastLZ output bound.
func (c *FastLZCompressor) MaxCompressedSize(bufferSize int32) int32 {
	if bufferSize < 0 {
		return 0
	}
	n := fastlz.MaxCompressedSize(int(bufferSize))
	if n > 1<<30 {
		return 0
	}
	return int32(n)
}

// Compress validates the declared capacities and reports compression failures.
func (c *FastLZCompressor) Compress(buffer []byte, bufferSize int32, compressed []byte, maxCompressedSize int32, compressedSize *int32) detour.DtStatus {
	if compressedSize == nil {
		return detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	*compressedSize = 0
	if bufferSize <= 0 || int64(bufferSize) > int64(len(buffer)) || maxCompressedSize < 0 || int64(maxCompressedSize) > int64(len(compressed)) {
		return detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	n := fastlz.Fastlz_compress(buffer, int(bufferSize), compressed[:maxCompressedSize])
	if n <= 0 {
		return detour.DT_FAILURE
	}
	*compressedSize = int32(n)
	return detour.DT_SUCCESS
}

// Decompress rejects corrupt streams, including FastLZ's zero-length failure result.
func (c *FastLZCompressor) Decompress(compressed []byte, compressedSize int32, buffer []byte, maxBufferSize int32, bufferSize *int32) detour.DtStatus {
	if bufferSize == nil {
		return detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	*bufferSize = 0
	if compressedSize <= 0 || int64(compressedSize) > int64(len(compressed)) || maxBufferSize < 0 || int64(maxBufferSize) > int64(len(buffer)) {
		return detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	n := fastlz.Fastlz_decompress(compressed, int(compressedSize), buffer, int(maxBufferSize))
	if n <= 0 {
		return detour.DT_FAILURE
	}
	*bufferSize = int32(n)
	return detour.DT_SUCCESS
}

const (
	POLYAREA_GROUND uint8 = 0
	POLYAREA_WATER  uint8 = 1
	POLYAREA_ROAD   uint8 = 2
	POLYAREA_DOOR   uint8 = 3
	POLYAREA_GRASS  uint8 = 4
	POLYAREA_JUMP   uint8 = 5
)

const (
	POLYFLAGS_WALK     uint16 = 0x01   // Ability to walk (ground, grass, road)
	POLYFLAGS_SWIM     uint16 = 0x02   // Ability to swim (water).
	POLYFLAGS_DOOR     uint16 = 0x04   // Ability to move through doors.
	POLYFLAGS_JUMP     uint16 = 0x08   // Ability to jump.
	POLYFLAGS_DISABLED uint16 = 0x10   // Disabled polygon
	POLYFLAGS_ALL      uint16 = 0xffff // All abilities.
)

type MeshProcess struct{}

func (p *MeshProcess) Process(params *detour.DtNavMeshCreateParams, polyAreas []uint8, polyFlags []uint16) {
	// Update poly flags from areas.
	for i := 0; i < int(params.PolyCount); i++ {
		if polyAreas[i] == dtcache.DT_TILECACHE_WALKABLE_AREA {
			polyAreas[i] = POLYAREA_GROUND
		}
		if polyAreas[i] == POLYAREA_GROUND ||
			polyAreas[i] == POLYAREA_GRASS ||
			polyAreas[i] == POLYAREA_ROAD {
			polyFlags[i] = POLYFLAGS_WALK
		} else if polyAreas[i] == POLYAREA_WATER {
			polyFlags[i] = POLYFLAGS_SWIM
		} else if polyAreas[i] == POLYAREA_DOOR {
			polyFlags[i] = POLYFLAGS_WALK | POLYFLAGS_DOOR
		}
	}
}

// DecodeTempObstacles validates a little-endian tile-cache mesh set. No partially
// loaded mesh or cache is returned when any layer or runtime tile fails.
func DecodeTempObstacles(data []byte) (*detour.DtNavMesh, *dtcache.DtTileCache, error) {
	if len(data) < 92 || len(data) > MaxMeshSetSize {
		return nil, nil, fmt.Errorf("loader: invalid tile-cache set size")
	}
	r := bytes.NewReader(data)
	var magic, version, count int32
	_ = binary.Read(r, binary.LittleEndian, &magic)
	_ = binary.Read(r, binary.LittleEndian, &version)
	_ = binary.Read(r, binary.LittleEndian, &count)
	if magic != TILECACHESET_MAGIC || version != TILECACHESET_VERSION || count < 0 {
		return nil, nil, fmt.Errorf("loader: invalid tile-cache set header")
	}
	var params detour.DtNavMeshParams
	var cp dtcache.DtTileCacheParams
	if err := binary.Read(r, binary.LittleEndian, &params); err != nil {
		return nil, nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &cp); err != nil {
		return nil, nil, err
	}
	if count > cp.MaxTiles || uint32(count) > params.MaxTiles || int64(count) > int64(r.Len())/8 {
		return nil, nil, fmt.Errorf("loader: inconsistent layer count")
	}
	mesh := detour.DtAllocNavMesh()
	if s := mesh.Init(&params); detour.DtStatusFailed(s) {
		return nil, nil, fmt.Errorf("loader: mesh init: %#x", s)
	}
	cache := dtcache.DtAllocTileCache()
	if s := cache.Init(&cp, &FastLZCompressor{}, &MeshProcess{}); detour.DtStatusFailed(s) {
		return nil, nil, fmt.Errorf("loader: cache init: %#x", s)
	}
	for i := int32(0); i < count; i++ {
		var ref uint32
		var size int32
		if err := binary.Read(r, binary.LittleEndian, &ref); err != nil {
			return nil, nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
			return nil, nil, err
		}
		if ref == 0 || size <= 0 || int64(size) > int64(r.Len()) {
			return nil, nil, fmt.Errorf("loader: invalid layer %d size or reference", i)
		}
		offset := len(data) - r.Len()
		var tile dtcache.DtCompressedTileRef
		if s := cache.AddTile(data[offset:offset+int(size)], size, dtcache.DT_COMPRESSEDTILE_FREE_DATA, &tile); detour.DtStatusFailed(s) {
			return nil, nil, fmt.Errorf("loader: layer %d add: %#x", i, s)
		}
		if s := cache.BuildNavMeshTile(tile, mesh); detour.DtStatusFailed(s) {
			return nil, nil, fmt.Errorf("loader: layer %d build: %#x", i, s)
		}
		_, _ = r.Seek(int64(size), 1)
	}
	if r.Len() != 0 {
		return nil, nil, fmt.Errorf("loader: trailing tile-cache bytes")
	}
	return mesh, cache, nil
}

// ReadTempObstacles reads a tile-cache set and preserves I/O and decoding errors.
func ReadTempObstacles(path string) (*detour.DtNavMesh, *dtcache.DtTileCache, error) {
	data, err := readBoundedFile(path)
	if err != nil {
		return nil, nil, err
	}
	return DecodeTempObstacles(data)
}

// LoadTempObstaclesByBytes returns nil values for any invalid or incomplete set.
func LoadTempObstaclesByBytes(data []byte) (*detour.DtNavMesh, *dtcache.DtTileCache) {
	mesh, cache, _ := DecodeTempObstacles(data)
	return mesh, cache
}

// LoadTempObstacles is the legacy file loader; errors return nil values.
func LoadTempObstacles(path string) (*detour.DtNavMesh, *dtcache.DtTileCache) {
	mesh, cache, _ := ReadTempObstacles(path)
	return mesh, cache
}
