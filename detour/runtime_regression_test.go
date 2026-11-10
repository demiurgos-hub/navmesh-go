package detour

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"unsafe"
)

func chainTile(t testing.TB, count int, disconnected, offmesh bool) []byte {
	t.Helper()
	p := DtNavMeshCreateParams{Nvp: 6, PolyCount: int32(count), VertCount: int32(count * 4), Cs: 1, Ch: 1, WalkableHeight: 2, WalkableRadius: .3, WalkableClimb: .5, Bmax: [3]float32{float32(count * 3), 3, 2}, BuildBvTree: true}
	p.PolyFlags = make([]uint16, count)
	p.PolyAreas = make([]uint8, count)
	p.Polys = make([]uint16, count*12)
	for i := range p.Polys {
		p.Polys[i] = 65535
	}
	for i := 0; i < count; i++ {
		x := i * 2
		if offmesh {
			x = i * 4
		}
		p.Verts = append(p.Verts, uint16(x), 0, 0, uint16(x), 0, 2, uint16(x+2), 0, 2, uint16(x+2), 0, 0)
		p.PolyFlags[i] = 1
		for j := 0; j < 4; j++ {
			p.Polys[i*12+j] = uint16(i*4 + j)
		}
		if !offmesh {
			if i > 0 && !(disconnected && i == count-1) {
				p.Polys[i*12+6] = uint16(i - 1)
			}
			if i < count-1 && !(disconnected && i == count-2) {
				p.Polys[i*12+8] = uint16(i + 1)
			}
		}
	}
	if offmesh {
		p.OffMeshConCount = 1
		p.OffMeshConVerts = []float32{1, 0, 1, 5, 0, 1}
		p.OffMeshConRad = []float32{.4}
		p.OffMeshConDir = []uint8{1}
		p.OffMeshConFlags = []uint16{1}
		p.OffMeshConAreas = []uint8{0}
		p.OffMeshConUserID = []uint32{123}
	}
	var data []byte
	var n int
	if !DtCreateNavMeshData(&p, &data, &n) {
		t.Fatal("tile construction failed")
	}
	if n != len(data) {
		t.Fatal("size mismatch")
	}
	return data
}

func queryForTile(t testing.TB, data []byte) (*DtNavMesh, *DtNavMeshQuery, DtPolyRef) {
	t.Helper()
	m := DtAllocNavMesh()
	if s := m.Init2(data, len(data), DT_TILE_FREE_DATA); DtStatusFailed(s) {
		t.Fatalf("load: %#x", s)
	}
	q := DtAllocNavMeshQuery()
	if s := q.Init(m, 128); DtStatusFailed(s) {
		t.Fatalf("query init: %#x", s)
	}
	return m, q, m.GetPolyRefBase(m.GetTile(0))
}

func TestPathOutcomesAndPrefixes(t *testing.T) {
	for _, sliced := range []bool{false, true} {
		for _, tc := range []struct {
			name                  string
			n, capacity           int
			disconnected, offmesh bool
			wantStatus            DtStatus
			offsets               []DtPolyRef
		}{
			{"complete", 5, 8, false, false, DT_SUCCESS, []DtPolyRef{0, 1, 2, 3, 4}},
			{"truncated-prefix", 5, 2, false, false, DT_SUCCESS | DT_BUFFER_TOO_SMALL, []DtPolyRef{0, 1}},
			{"exact-capacity", 5, 5, false, false, DT_SUCCESS, []DtPolyRef{0, 1, 2, 3, 4}},
			{"partial", 5, 8, true, false, DT_SUCCESS | DT_PARTIAL_RESULT, []DtPolyRef{0, 1, 2, 3}},
			{"offmesh", 2, 8, false, true, DT_SUCCESS, []DtPolyRef{0, 2, 1}},
		} {
			t.Run(tc.name+map[bool]string{true: "/sliced", false: "/ordinary"}[sliced], func(t *testing.T) {
				_, q, base := queryForTile(t, chainTile(t, tc.n, tc.disconnected, tc.offmesh))
				start, end := []float32{1, 0, 1}, []float32{float32(tc.n*2 - 1), 0, 1}
				if tc.offmesh {
					end[0] = 5
				}
				path := make([]DtPolyRef, tc.capacity)
				var n int
				var status DtStatus
				f := DtAllocDtQueryFilter()
				if sliced {
					status = q.InitSlicedFindPath(base, base+DtPolyRef(tc.n-1), start, end, f, 0)
					for i := 0; DtStatusInProgress(status) && i < 100; i++ {
						status = q.UpdateSlicedFindPath(1, nil)
					}
					status = q.FinalizeSlicedFindPath(path, &n, len(path))
				} else {
					status = q.FindPath(base, base+DtPolyRef(tc.n-1), start, end, f, path, &n, len(path))
				}
				want := append([]DtPolyRef(nil), tc.offsets...)
				for i := range want {
					want[i] += base
				}
				if status != tc.wantStatus || !reflect.DeepEqual(path[:n], want) {
					t.Fatalf("status=%#x path=%v; want %#x %v", status, path[:n], tc.wantStatus, want)
				}
			})
		}
	}
}

func TestTileValidationAndFailureAtomicity(t *testing.T) {
	data := chainTile(t, 5, false, false)
	for _, n := range []int{0, 1, 4, 99, 100, len(data) - 1} {
		if err := ValidateNavMeshData(data[:n]); err == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
	for _, offset := range []int{24, 28, 32, 36, 40, 44, 48, 52, 56} {
		bad := append([]byte(nil), data...)
		binary.LittleEndian.PutUint32(bad[offset:], 0x7fffffff)
		if err := ValidateNavMeshData(bad); err == nil {
			t.Fatalf("accepted count at %d", offset)
		}
	}
	m := DtAllocNavMesh()
	if s := m.Init(&DtNavMeshParams{TileWidth: 16, TileHeight: 16, MaxTiles: 1, MaxPolys: 16}); DtStatusFailed(s) {
		t.Fatal(s)
	}
	for _, bad := range [][]byte{data[:100], nil} {
		if s := m.AddTile(bad, len(bad), 0, 0, nil); !DtStatusFailed(s) {
			t.Fatal("accepted invalid tile")
		}
	}
	var ref DtTileRef
	if s := m.AddTile(data, len(data), 0, 0, &ref); DtStatusFailed(s) {
		t.Fatal("failed add consumed capacity", s)
	}
	if s := m.ReplaceTile(data[:100], 100, 0, nil); !DtStatusFailed(s) || m.GetTileByRef(ref) == nil {
		t.Fatal("failed replacement changed mesh")
	}
	if s := m.AddTile(data[:10], len(data), 0, 0, nil); !DtStatusFailed(s) {
		t.Fatal("accepted declared size mismatch")
	}
}

func TestTileCodecOwnsStorageAndPreservesReferences(t *testing.T) {
	data := chainTile(t, 5, false, false)
	decoded, err := DecodeNavMeshData(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeNavMeshData(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, again) {
		t.Fatal("noncanonical round trip")
	}
	m, q, base := queryForTile(t, data)
	for i := range data {
		data[i] = 0
	}
	var nearest DtPolyRef
	if s := q.FindNearestPoly([]float32{1, 0, 1}, []float32{.1, .1, .1}, DtAllocDtQueryFilter(), &nearest, nil); DtStatusFailed(s) || nearest != base {
		t.Fatal("mesh borrowed input storage", s, nearest)
	}
	ref := m.GetTileRef(m.GetTile(0))
	if s := m.RemoveTile(ref, nil, nil); DtStatusFailed(s) {
		t.Fatal(s)
	}
	if m.IsValidPolyRef(base) {
		t.Fatal("stale polygon remained valid")
	}
	if s := m.AddTile(again, len(again), 0, 0, nil); DtStatusFailed(s) {
		t.Fatal(s)
	}
	if m.IsValidPolyRef(base) {
		t.Fatal("old salt reused")
	}
}

func TestQueryRejectsInvalidBuffers(t *testing.T) {
	_, q, base := queryForTile(t, chainTile(t, 5, false, false))
	f := DtAllocDtQueryFilter()
	start, end := []float32{1, 0, 1}, []float32{9, 0, 1}
	var n int
	if s := q.FindPath(base, base+4, start, end, f, make([]DtPolyRef, 1), &n, 8); s != DT_FAILURE|DT_INVALID_PARAM || n != 0 {
		t.Fatal(s, n)
	}
	for _, pos := range [][]float32{nil, {1}, {1, 2}, {float32(math.NaN()), 0, 0}, {0, float32(math.Inf(1)), 0}} {
		if s := q.FindPath(base, base+4, pos, end, f, make([]DtPolyRef, 8), &n, 8); !DtStatusFailed(s) {
			t.Fatal("accepted invalid position")
		}
		if s := q.InitSlicedFindPath(base, base+4, pos, end, f, 0); !DtStatusFailed(s) {
			t.Fatal("accepted invalid sliced position")
		}
	}
	if s := q.FinalizeSlicedFindPath(nil, nil, 0); !DtStatusFailed(s) {
		t.Fatal(s)
	}
	if s := q.FindStraightPath(start, end, nil, 0, nil, nil, nil, nil, 0, 0); !DtStatusFailed(s) {
		t.Fatal(s)
	}
	if s := q.MoveAlongSurface(base, start, end, f, nil, nil, nil, 1, nil); !DtStatusFailed(s) {
		t.Fatal(s)
	}
	if s := q.Raycast2(base, start, end, f, 0, nil, 0); !DtStatusFailed(s) {
		t.Fatal(s)
	}
	var nearest DtPolyRef
	if s := q.FindNearestPoly(start, []float32{1, -1, 1}, f, &nearest, nil); !DtStatusFailed(s) {
		t.Fatal(s)
	}
}

func TestNodePoolMemoryAccounting(t *testing.T) {
	p := DtAllocNodePool(128, 32)
	want := uint32(unsafe.Sizeof(*p) + 128*unsafe.Sizeof(DtNode{}) + 160*unsafe.Sizeof(DtNodeIndex(0)))
	if p.GetMemUsed() != want {
		t.Fatalf("got %d want %d", p.GetMemUsed(), want)
	}
}

func TestEndianAndTileStateEncoding(t *testing.T) {
	data := chainTile(t, 2, false, true)
	swapped := append([]byte(nil), data...)
	if !DtNavMeshDataSwapEndian(swapped, len(swapped)) || !DtNavMeshHeaderSwapEndian(swapped, len(swapped)) || !DtNavMeshHeaderSwapEndian(swapped, len(swapped)) || !DtNavMeshDataSwapEndian(swapped, len(swapped)) || !reflect.DeepEqual(data, swapped) {
		t.Fatal("endian round trip failed")
	}
	for _, short := range [][]byte{nil, data[:99], data[:100]} {
		if DtNavMeshDataSwapEndian(short, len(short)) {
			t.Fatal("accepted short tile body")
		}
	}
	m, _, _ := queryForTile(t, data)
	tile := m.GetTile(0)
	state := make([]byte, m.GetTileStateSize(tile))
	if s := m.StoreTileState(tile, state, len(state)); DtStatusFailed(s) {
		t.Fatal(s)
	}
	tile.Polys[0].Flags = 0
	tile.Polys[0].SetArea(7)
	if s := m.RestoreTileState(tile, state, len(state)); DtStatusFailed(s) || tile.Polys[0].Flags != 1 || tile.Polys[0].GetArea() != 0 {
		t.Fatal("state restore", s)
	}
	bad := append([]byte(nil), state...)
	bad[14] = 64
	if s := m.RestoreTileState(tile, bad, len(bad)); !DtStatusFailed(s) || tile.Polys[0].Flags != 1 {
		t.Fatal("malformed state changed tile")
	}
	if s := m.StoreTileState(tile, nil, len(state)); !DtStatusFailed(s) {
		t.Fatal("accepted wrong capacity")
	}
}

func FuzzDecodeNavMeshData(f *testing.F) {
	f.Add(chainTile(f, 5, false, false))
	f.Add([]byte{})
	f.Add(chainTile(f, 2, false, true))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		tile, err := DecodeNavMeshData(b)
		if err != nil {
			return
		}
		encoded, err := EncodeNavMeshData(tile)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateNavMeshData(encoded); err != nil {
			t.Fatal(err)
		}
	})
}
