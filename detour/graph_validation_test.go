package detour

import (
	"encoding/binary"
	"math"
	"testing"
)

func graphParams() DtNavMeshCreateParams {
	p := DtNavMeshCreateParams{Nvp: 4, PolyCount: 2, VertCount: 8, Cs: 1, Ch: 1, WalkableHeight: 2, Bmax: [3]float32{4, 3, 2}}
	p.Verts = []uint16{0, 0, 0, 0, 0, 2, 2, 0, 2, 2, 0, 0, 2, 0, 0, 2, 0, 2, 4, 0, 2, 4, 0, 0}
	p.PolyFlags = []uint16{1, 1}
	p.PolyAreas = []uint8{1, 1}
	p.Polys = []uint16{0, 1, 2, 3, 65535, 65535, 1, 65535, 4, 5, 6, 7, 0, 65535, 65535, 65535}
	return p
}

func TestConstructionRejectsInvalidGroundGraph(t *testing.T) {
	p := graphParams()
	if err := ValidateCreateParams(&p); err != nil {
		t.Fatalf("valid shared edge with duplicated vertices rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*DtNavMeshCreateParams)
	}{
		{"self-neighbor", func(p *DtNavMeshCreateParams) { p.Polys[6] = 0 }},
		{"nonreciprocal", func(p *DtNavMeshCreateParams) { p.Polys[12] = 65535 }},
		{"different-edge", func(p *DtNavMeshCreateParams) {
			for i := 4; i < 8; i++ {
				p.Verts[i*3]++
			}
		}},
		{"nonconvex", func(p *DtNavMeshCreateParams) { p.Verts[6], p.Verts[8] = 0, 1 }},
		{"zero-area", func(p *DtNavMeshCreateParams) {
			for i := 0; i < 4; i++ {
				p.Verts[i*3+2] = 0
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := graphParams()
			tc.edit(&p)
			if ValidateCreateParams(&p) == nil {
				t.Fatal("accepted invalid ground graph")
			}
		})
	}
}

func TestLegacyTileRejectsInvalidGroundGraph(t *testing.T) {
	data := chainTile(t, 2, false, false)
	header, err := tileHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	polyStart := NavMeshHeaderSize + int(header.VertCount)*12
	for _, tc := range []struct {
		name string
		edit func([]byte)
	}{
		{"self-neighbor", func(b []byte) { binary.LittleEndian.PutUint16(b[polyStart+16+2*2:], 1) }},
		{"nonreciprocal", func(b []byte) { binary.LittleEndian.PutUint16(b[polyStart+32+16:], 0) }},
		{"different-edge", func(b []byte) {
			binary.LittleEndian.PutUint32(b[NavMeshHeaderSize+4*12:], math.Float32bits(3))
		}},
		{"nonconvex", func(b []byte) {
			binary.LittleEndian.PutUint32(b[NavMeshHeaderSize+2*12:], math.Float32bits(.5))
			binary.LittleEndian.PutUint32(b[NavMeshHeaderSize+2*12+8:], math.Float32bits(.5))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte(nil), data...)
			tc.edit(b)
			if ValidateNavMeshData(b) == nil {
				t.Fatal("accepted invalid encoded ground graph")
			}
		})
	}
}

func TestRaycastRejectsRuntimeCycles(t *testing.T) {
	for _, linkCycle := range []bool{false, true} {
		m, q, base := queryForTile(t, chainTile(t, 1, false, false))
		tile := m.GetTile(0)
		tile.Polys[0].FirstLink = 0
		tile.Links[0] = DtLink{Ref: base, Edge: 2, Side: 0xff, Next: DT_NULL_LINK}
		if linkCycle {
			tile.Links[0].Edge = 0 // Not the crossed edge: exercise link iteration.
			tile.Links[0].Next = 0
		}
		hit := DtRaycastHit{Path: make([]DtPolyRef, 4), MaxPath: 4}
		st := q.Raycast2(base, []float32{1, 0, 1}, []float32{4, 0, 1}, DtAllocDtQueryFilter(), 0, &hit, 0)
		if !DtStatusFailed(st) || !DtStatusDetail(st, DT_INVALID_PARAM) {
			t.Fatalf("runtime cycle accepted: %#x", st)
		}
	}
}

func TestRaycastLastRefSurvivesPathTruncation(t *testing.T) {
	_, q, base := queryForTile(t, chainTile(t, 5, false, false))
	hit := DtRaycastHit{Path: make([]DtPolyRef, 1), MaxPath: 1}
	st := q.Raycast2(base, []float32{1, 0, 1}, []float32{9, 0, 1}, DtAllocDtQueryFilter(), 0, &hit, 0)
	if DtStatusFailed(st) || !DtStatusDetail(st, DT_BUFFER_TOO_SMALL) || hit.PathCount != 1 || hit.Path[0] != base || hit.LastRef != base+4 || hit.T != math.MaxFloat32 {
		t.Fatalf("truncated ray lost final surface or prefix: status=%#x hit=%+v", st, hit)
	}
}
