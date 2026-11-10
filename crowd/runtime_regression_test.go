package dtcrowd

import (
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/loader"
)

func TestFirstPathQueueRequestAndFilterOwnership(t *testing.T) {
	m, err := loader.ReadTileMesh("../examples/assets/navmesh.bin")
	if err != nil {
		t.Fatal(err)
	}
	var tile *detour.DtMeshTile
	for i := 0; i < int(m.GetMaxTiles()); i++ {
		if m.GetTile(i).Header != nil {
			tile = m.GetTile(i)
			break
		}
	}
	if tile == nil {
		t.Fatal("missing tile")
	}
	ref := m.GetPolyRefBase(tile)
	pos := append([]float32(nil), tile.Verts[int(tile.Polys[0].Verts[0])*3:int(tile.Polys[0].Verts[0])*3+3]...)
	q := new(DtPathQueue)
	if !q.Init(16, 128, m) {
		t.Fatal("init")
	}
	filter := detour.DtAllocDtQueryFilter()
	r := q.Request(ref, ref, pos, pos, filter)
	if r == DT_PATHQ_INVALID {
		t.Fatal("first request returned invalid handle")
	}
	filter.SetIncludeFlags(0)
	if q.m_queue[0].filter.GetIncludeFlags() == 0 {
		t.Fatal("request borrowed mutable filter")
	}
	q.Update(16)
	if !detour.DtStatusSucceed(q.GetRequestStatus(r)) {
		t.Fatal("request did not complete")
	}
	var n int
	path := make([]detour.DtPolyRef, 16)
	if s := q.GetPathResult(r, path, &n, len(path)); !detour.DtStatusSucceed(s) || n != 1 || path[0] != ref {
		t.Fatal(s, n, path)
	}
	if q.Request(ref, ref, nil, pos, filter) != DT_PATHQ_INVALID {
		t.Fatal("accepted short input")
	}
	if s := q.GetPathResult(r, nil, nil, 1); !detour.DtStatusFailed(s) {
		t.Fatal("accepted invalid output")
	}
}

func TestObstacleAvoidanceParameterBoundary(t *testing.T) {
	c := new(DtCrowd)
	for _, index := range []int{-1, DT_CROWD_MAX_OBSTAVOIDANCE_PARAMS, DT_CROWD_MAX_OBSTAVOIDANCE_PARAMS + 1} {
		c.SetObstacleAvoidanceParams(index, new(DtObstacleAvoidanceParams))
		if c.GetObstacleAvoidanceParams(index) != nil {
			t.Fatal(index)
		}
	}
	c.SetObstacleAvoidanceParams(0, nil)
}
