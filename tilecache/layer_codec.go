package dtcache

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/demiurgos-hub/navmesh-go/detour"
)

// LayerHeaderSize is the fixed encoded size, including two reserved padding bytes.
const LayerHeaderSize = 56

// MaxLayerDataSize bounds untrusted compressed layers before allocation.
const MaxLayerDataSize = 1 << 20

func finite(v float32) bool    { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func finite3(v []float32) bool { return len(v) >= 3 && finite(v[0]) && finite(v[1]) && finite(v[2]) }
func validBounds(min, max []float32) bool {
	return finite3(min) && finite3(max) && min[0] <= max[0] && min[1] <= max[1] && min[2] <= max[2]
}
func validLayerHeader(h *DtTileCacheLayerHeader) bool {
	return h != nil && h.Magic == DT_TILECACHE_MAGIC && h.Version == DT_TILECACHE_VERSION && h.Width > 0 && h.Height > 0 && h.Minx <= h.Maxx && h.Maxx < h.Width && h.Miny <= h.Maxy && h.Maxy < h.Height && h.Hmin <= h.Hmax && h.Tlayer >= 0 && validBounds(h.Bmin[:], h.Bmax[:])
}
func decodeLayerHeader(data []byte) (*DtTileCacheLayerHeader, detour.DtStatus) {
	if len(data) <= LayerHeaderSize || len(data) > MaxLayerDataSize {
		return nil, detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	h := new(DtTileCacheLayerHeader)
	_ = binary.Read(bytes.NewReader(data[:54]), binary.LittleEndian, h)
	if h.Magic != DT_TILECACHE_MAGIC {
		return nil, detour.DT_FAILURE | detour.DT_WRONG_MAGIC
	}
	if h.Version != DT_TILECACHE_VERSION {
		return nil, detour.DT_FAILURE | detour.DT_WRONG_VERSION
	}
	if !validLayerHeader(h) {
		return nil, detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	return h, detour.DT_SUCCESS
}
func encodeLayerHeader(data []byte, h *DtTileCacheLayerHeader) {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, h)
	copy(data[:LayerHeaderSize], b.Bytes())
}
