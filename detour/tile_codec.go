package detour

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// NavMeshHeaderSize is the encoded size of a 32-bit-reference Detour v7 header.
const NavMeshHeaderSize = 100

// MaxTileDataSize bounds allocations when decoding an untrusted tile.
const MaxTileDataSize = 64 << 20

var errTileMagic = errors.New("detour: wrong tile magic")
var errTileVersion = errors.New("detour: unsupported tile version")

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func finite3(v []float32) bool {
	return len(v) >= 3 && finite(v[0]) && finite(v[1]) && finite(v[2])
}

func tileHeader(data []byte) (DtMeshHeader, error) {
	var h DtMeshHeader
	if len(data) < NavMeshHeaderSize || len(data) > MaxTileDataSize {
		return h, errors.New("detour: invalid tile length")
	}
	if err := binary.Read(bytes.NewReader(data[:NavMeshHeaderSize]), binary.LittleEndian, &h); err != nil {
		return h, err
	}
	if h.Magic != DT_NAVMESH_MAGIC {
		return h, errTileMagic
	}
	if h.Version != DT_NAVMESH_VERSION {
		return h, errTileVersion
	}
	return h, nil
}

func tileEncodedSize(h *DtMeshHeader) (int, error) {
	counts := []int32{h.VertCount, h.PolyCount, h.MaxLinkCount, h.DetailMeshCount, h.DetailVertCount, h.DetailTriCount, h.BvNodeCount, h.OffMeshConCount}
	sizes := []int64{12, 32, 12, 12, 12, 4, 16, 36}
	total := int64(NavMeshHeaderSize)
	for i, n := range counts {
		if n < 0 {
			return 0, errors.New("detour: negative tile section count")
		}
		total += int64(n) * sizes[i]
		if total > MaxTileDataSize {
			return 0, errors.New("detour: tile exceeds allocation limit")
		}
	}
	return int(total), nil
}

// ValidateNavMeshData validates a complete little-endian Detour v7 tile with
// 32-bit references. It never borrows storage from data or changes a mesh.
func ValidateNavMeshData(data []byte) error {
	_, err := DecodeNavMeshData(data)
	return err
}

// DecodeNavMeshData decodes and validates a tile into independent Go-owned
// arrays. Native struct layout, native byte order and pointer width are unused.
// Runtime links are initialized by AddTile; encoded link records are ignored.
func DecodeNavMeshData(data []byte) (*DtMeshTile, error) {
	h, err := tileHeader(data)
	if err != nil {
		return nil, err
	}
	size, err := tileEncodedSize(&h)
	if err != nil {
		return nil, err
	}
	if size != len(data) {
		return nil, fmt.Errorf("detour: tile length %d, expected %d", len(data), size)
	}
	if h.PolyCount <= 0 || h.PolyCount > 65535 || h.VertCount < 3 || h.VertCount > 65535 || h.MaxLinkCount <= 0 || h.OffMeshBase <= 0 || h.OffMeshBase > h.PolyCount || h.OffMeshConCount != h.PolyCount-h.OffMeshBase || h.DetailMeshCount != h.OffMeshBase || h.Layer < 0 {
		return nil, errors.New("detour: inconsistent tile counts")
	}
	for _, v := range []float32{h.WalkableHeight, h.WalkableRadius, h.WalkableClimb} {
		if !finite(v) || v < 0 {
			return nil, errors.New("detour: invalid agent dimensions")
		}
	}
	if !finite3(h.Bmin[:]) || !finite3(h.Bmax[:]) || !finite(h.BvQuantFactor) || h.BvQuantFactor <= 0 {
		return nil, errors.New("detour: invalid tile bounds or quantization")
	}
	for i := range h.Bmin {
		if h.Bmin[i] > h.Bmax[i] {
			return nil, errors.New("detour: reversed tile bounds")
		}
	}
	t := &DtMeshTile{Header: &h, Verts: make([]float32, int(h.VertCount)*3), Polys: make([]DtPoly, h.PolyCount), Links: make([]DtLink, h.MaxLinkCount), DetailMeshes: make([]DtPolyDetail, h.DetailMeshCount), DetailVerts: make([]float32, int(h.DetailVertCount)*3), DetailTris: make([]byte, int(h.DetailTriCount)*4), BvTree: make([]DtBVNode, h.BvNodeCount), OffMeshCons: make([]DtOffMeshConnection, h.OffMeshConCount)}
	r := bytes.NewReader(data[NavMeshHeaderSize:])
	// Section lengths were checked before allocations, so each read is bounded.
	read := func(v interface{}) { _ = binary.Read(r, binary.LittleEndian, v) }
	read(t.Verts)
	read(t.Polys)
	_, _ = r.Seek(int64(h.MaxLinkCount)*12, 1)
	for i := range t.DetailMeshes {
		var w struct {
			VertBase, TriBase   uint32
			VertCount, TriCount uint8
			Padding             [2]byte
		}
		read(&w)
		t.DetailMeshes[i] = DtPolyDetail{VertBase: w.VertBase, TriBase: w.TriBase, VertCount: w.VertCount, TriCount: w.TriCount}
	}
	read(t.DetailVerts)
	read(t.DetailTris)
	read(t.BvTree)
	read(t.OffMeshCons)
	for _, vs := range [][]float32{t.Verts, t.DetailVerts} {
		for _, v := range vs {
			if !finite(v) {
				return nil, errors.New("detour: non-finite vertex")
			}
		}
	}
	for i := range t.Polys {
		p := &t.Polys[i]
		ground := i < int(h.OffMeshBase)
		if ground && (p.GetType() != DT_POLYTYPE_GROUND || p.VertCount < 3 || int32(p.VertCount) > DT_VERTS_PER_POLYGON) || !ground && (p.GetType() != DT_POLYTYPE_OFFMESH_CONNECTION || p.VertCount != 2) {
			return nil, errors.New("detour: invalid polygon type or vertex count")
		}
		for j := 0; j < int(p.VertCount); j++ {
			if int32(p.Verts[j]) >= h.VertCount {
				return nil, errors.New("detour: polygon vertex out of range")
			}
			n := p.Neis[j]
			if n&DT_EXT_LINK != 0 {
				if n&^uint16(DT_EXT_LINK|7) != 0 {
					return nil, errors.New("detour: invalid portal side")
				}
			} else if int32(n) > h.OffMeshBase {
				return nil, errors.New("detour: polygon neighbor out of range")
			}
		}
		p.FirstLink = DT_NULL_LINK
		if !ground {
			continue
		}
		d := t.DetailMeshes[i]
		if uint64(d.VertBase)+uint64(d.VertCount) > uint64(h.DetailVertCount) || uint64(d.TriBase)+uint64(d.TriCount) > uint64(h.DetailTriCount) || d.TriCount == 0 {
			return nil, errors.New("detour: invalid detail mesh range")
		}
		for k := uint32(0); k < uint32(d.TriCount); k++ {
			tri := t.DetailTris[int(d.TriBase+k)*4:]
			for j := 0; j < 3; j++ {
				if int(tri[j]) >= int(p.VertCount)+int(d.VertCount) {
					return nil, errors.New("detour: detail triangle vertex out of range")
				}
			}
		}
	}
	if !validGroundGraph(int(h.OffMeshBase), func(i int) groundPolygon {
		p := &t.Polys[i]
		out := groundPolygon{count: int(p.VertCount)}
		for j := 0; j < out.count; j++ {
			v := int(p.Verts[j]) * 3
			out.vertices[j] = [3]float64{float64(t.Verts[v]), float64(t.Verts[v+1]), float64(t.Verts[v+2])}
			out.neighbors[j] = -1
			if n := p.Neis[j]; n != 0 && n&DT_EXT_LINK == 0 {
				out.neighbors[j] = int(n) - 1
			}
		}
		return out
	}) {
		return nil, errors.New("detour: invalid ground polygon or inconsistent adjacency")
	}
	for i, n := range t.BvTree {
		for axis := 0; axis < 3; axis++ {
			if n.Bmin[axis] > n.Bmax[axis] {
				return nil, errors.New("detour: reversed BV bounds")
			}
		}
		if n.I >= 0 {
			if n.I >= h.OffMeshBase {
				return nil, errors.New("detour: BV leaf out of range")
			}
		} else if -int64(n.I) > int64(len(t.BvTree)-i) {
			return nil, errors.New("detour: BV escape out of range")
		}
	}
	for i, c := range t.OffMeshCons {
		if int32(c.Poly) != h.OffMeshBase+int32(i) || !finite3(c.Pos[:3]) || !finite3(c.Pos[3:]) || !finite(c.Rad) || c.Rad < 0 || c.Flags&^uint8(DT_OFFMESH_CON_BIDIR) != 0 || c.Side != 0xff && c.Side > 7 {
			return nil, errors.New("detour: invalid off-mesh connection")
		}
	}
	if len(t.BvTree) == 0 {
		t.BvTree = nil
	}
	t.Data = append([]byte(nil), data...)
	t.DataSize = int32(len(data))
	return t, nil
}

// EncodeNavMeshData encodes a tile using the explicit little-endian Detour v7
// layout with 32-bit references. Runtime adjacency is omitted and rebuilt on load.
func EncodeNavMeshData(t *DtMeshTile) ([]byte, error) {
	if t == nil || t.Header == nil {
		return nil, errors.New("detour: missing tile")
	}
	h := t.Header
	size, err := tileEncodedSize(h)
	if err != nil {
		return nil, err
	}
	if int64(len(t.Verts)) != int64(h.VertCount)*3 || int64(len(t.Polys)) != int64(h.PolyCount) || int64(len(t.DetailMeshes)) != int64(h.DetailMeshCount) || int64(len(t.DetailVerts)) != int64(h.DetailVertCount)*3 || int64(len(t.DetailTris)) != int64(h.DetailTriCount)*4 || int64(len(t.BvTree)) != int64(h.BvNodeCount) || int64(len(t.OffMeshCons)) != int64(h.OffMeshConCount) {
		return nil, errors.New("detour: tile section length mismatch")
	}
	b := bytes.NewBuffer(make([]byte, 0, size))
	write := func(v interface{}) { _ = binary.Write(b, binary.LittleEndian, v) }
	write(h)
	write(t.Verts)
	for _, p := range t.Polys {
		p.FirstLink = 0
		write(p)
	}
	b.Write(make([]byte, int(h.MaxLinkCount)*12))
	for _, d := range t.DetailMeshes {
		write(d)
		b.Write([]byte{0, 0})
	}
	write(t.DetailVerts)
	write(t.DetailTris)
	write(t.BvTree)
	write(t.OffMeshCons)
	if err := ValidateNavMeshData(b.Bytes()); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func tileErrorStatus(err error) DtStatus {
	if errors.Is(err, errTileMagic) {
		return DT_FAILURE | DT_WRONG_MAGIC
	}
	if errors.Is(err, errTileVersion) {
		return DT_FAILURE | DT_WRONG_VERSION
	}
	return DT_FAILURE | DT_INVALID_PARAM
}
