package loader

import (
	"bytes"
	"encoding/binary"
	"fmt"

	detour "github.com/demiurgos-hub/navmesh-go/detour"
)

const NAVMESHSET_MAGIC = 'M'<<24 | 'S'<<16 | 'E'<<8 | 'T' //'MSET';
const NAVMESHSET_VERSION = 1

type NavMeshSetHeader struct {
	magic    int32
	version  int32
	numTiles int32
	params   detour.DtNavMeshParams
}

type NavMeshTileHeader struct {
	tileRef  detour.DtTileRef
	dataSize int32
}

// DecodeTileMesh validates a complete little-endian mesh set before returning it.
func DecodeTileMesh(data []byte) (*detour.DtNavMesh, error) {
	if len(data) < 40 || len(data) > MaxMeshSetSize {
		return nil, fmt.Errorf("loader: invalid mesh-set size")
	}
	r := bytes.NewReader(data)
	var magic, version, count int32
	_ = binary.Read(r, binary.LittleEndian, &magic)
	_ = binary.Read(r, binary.LittleEndian, &version)
	_ = binary.Read(r, binary.LittleEndian, &count)
	if magic != NAVMESHSET_MAGIC || version != NAVMESHSET_VERSION || count < 0 {
		return nil, fmt.Errorf("loader: invalid mesh-set header")
	}
	var params detour.DtNavMeshParams
	if err := binary.Read(r, binary.LittleEndian, &params); err != nil {
		return nil, err
	}
	if uint32(count) > params.MaxTiles || int64(count) > int64(r.Len())/8 {
		return nil, fmt.Errorf("loader: inconsistent tile count")
	}
	mesh := detour.DtAllocNavMesh()
	if status := mesh.Init(&params); detour.DtStatusFailed(status) {
		return nil, fmt.Errorf("loader: mesh initialization failed: %#x", status)
	}
	for i := int32(0); i < count; i++ {
		var ref uint32
		var size int32
		if err := binary.Read(r, binary.LittleEndian, &ref); err != nil {
			return nil, fmt.Errorf("loader: tile %d header: %w", i, err)
		}
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
			return nil, err
		}
		if ref == 0 || size <= 0 || int64(size) > int64(r.Len()) || size > detour.MaxTileDataSize {
			return nil, fmt.Errorf("loader: tile %d length or reference is invalid", i)
		}
		offset := len(data) - r.Len()
		status := mesh.AddTile(data[offset:offset+int(size)], int(size), detour.DT_TILE_FREE_DATA, detour.DtTileRef(ref), nil)
		if detour.DtStatusFailed(status) {
			return nil, fmt.Errorf("loader: tile %d failed validation/loading: %#x", i, status)
		}
		_, _ = r.Seek(int64(size), 1)
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("loader: trailing mesh-set bytes")
	}
	return mesh, nil
}

// ReadTileMesh reads and validates a mesh-set file, preserving I/O diagnostics.
func ReadTileMesh(path string) (*detour.DtNavMesh, error) {
	data, err := readBoundedFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeTileMesh(data)
}

// LoadTileMeshByBytes is the legacy loader; malformed data returns nil.
// Use DecodeTileMesh when an explanatory error is needed.
func LoadTileMeshByBytes(data []byte) *detour.DtNavMesh { mesh, _ := DecodeTileMesh(data); return mesh }

// LoadTileMesh is the legacy file loader; I/O or decoding failures return nil.
func LoadTileMesh(path string) *detour.DtNavMesh { mesh, _ := ReadTileMesh(path); return mesh }
