package detour

import "testing"

func TestLazyQueryPools(t *testing.T) {
	m, q, ref := queryForTile(t, chainTile(t, 8, false, false))
	if q.m_nodePool != nil || q.m_openList != nil || q.m_tinyNodePool != nil || q.GetNodePool() != nil {
		t.Fatal("Init allocated search scratch")
	}
	filter := DtAllocDtQueryFilter()
	a, b := []float32{1, 0, 1}, []float32{15, 0, 1}
	var point [3]float32
	var nearest DtPolyRef
	if st := q.FindNearestPoly(a, []float32{1, 1, 1}, filter, &nearest, point[:]); DtStatusFailed(st) || nearest != ref {
		t.Fatalf("projection: %#x, %d", st, nearest)
	}
	if q.m_nodePool != nil || q.m_openList != nil || q.m_tinyNodePool != nil {
		t.Fatal("projection allocated search pools")
	}
	var path [16]DtPolyRef
	var count int
	if st := q.GetPathFromDijkstraSearch(ref, path[:], &count, len(path)); !DtStatusFailed(st) {
		t.Fatal("unstarted Dijkstra search accepted")
	}
	if q.IsInClosedList(ref) || q.m_nodePool != nil {
		t.Fatal("inspection created a search pool")
	}
	var hit bool
	if st := q.MoveAlongSurface(ref, a, []float32{1.1, 0, 1}, filter, point[:], path[:], &count, len(path), &hit); DtStatusFailed(st) {
		t.Fatalf("movement: %#x", st)
	}
	if q.m_tinyNodePool == nil || q.m_nodePool != nil || q.m_openList != nil {
		t.Fatal("movement did not use only the tiny pool")
	}
	tiny := q.m_tinyNodePool
	if st := q.FindPath(ref, ref+7, a, b, filter, path[:], &count, len(path)); DtStatusFailed(st) || count != 8 {
		t.Fatalf("search: %#x, %d", st, count)
	}
	pool, queue := q.m_nodePool, q.m_openList
	if pool == nil || queue == nil || pool.GetMaxNodes() != 128 || q.m_tinyNodePool != tiny {
		t.Fatal("full search scratch is missing or replaced the tiny pool")
	}
	if st := q.Init(m, 128); DtStatusFailed(st) || q.m_nodePool != pool || q.m_openList != queue || q.m_tinyNodePool != tiny {
		t.Fatal("same-limit initialization did not reuse scratch")
	}
	if q.IsInClosedList(ref) || q.m_query.status != 0 || pool.GetNodeCount() != 0 {
		t.Fatal("initialization retained search state")
	}
	if st := q.Init(m, 4); DtStatusFailed(st) || q.m_nodePool != nil || q.m_openList != nil {
		t.Fatal("smaller limit retained an oversized search pool")
	}
	if st := q.FindPath(ref, ref+7, a, b, filter, path[:], &count, len(path)); !DtStatusDetail(st, DT_OUT_OF_NODES) || q.m_nodePool.GetMaxNodes() != 4 || count > 4 {
		t.Fatalf("new node bound not enforced: %#x, count=%d", st, count)
	}
	DtFreeNavMeshQuery(q)
	if q.ready() || q.GetNodePool() != nil || q.m_tinyNodePool != nil || q.m_raycastVisited != nil {
		t.Fatal("free retained initialized scratch")
	}
}

func TestSearchEntryPointsAllocateLazyPools(t *testing.T) {
	for name, run := range map[string]func(*DtNavMeshQuery, DtPolyRef, *DtQueryFilter) DtStatus{
		"sliced": func(q *DtNavMeshQuery, ref DtPolyRef, f *DtQueryFilter) DtStatus {
			st := q.InitSlicedFindPath(ref, ref+3, []float32{1, 0, 1}, []float32{7, 0, 1}, f, DT_FINDPATH_ANY_ANGLE)
			for DtStatusInProgress(st) {
				st = q.UpdateSlicedFindPath(1, nil)
			}
			var path [8]DtPolyRef
			var count int
			return q.FinalizeSlicedFindPath(path[:], &count, len(path))
		},
		"random-circle": func(q *DtNavMeshQuery, ref DtPolyRef, f *DtQueryFilter) DtStatus {
			var found DtPolyRef
			var point [3]float32
			return q.FindRandomPointAroundCircle(ref, []float32{1, 0, 1}, 10, f, func() float32 { return .5 }, &found, point[:])
		},
		"circle": func(q *DtNavMeshQuery, ref DtPolyRef, f *DtQueryFilter) DtStatus {
			var refs, parents [8]DtPolyRef
			var costs [8]float32
			var count int
			return q.FindPolysAroundCircle(ref, []float32{1, 0, 1}, 10, f, refs[:], parents[:], costs[:], &count, len(refs))
		},
		"shape": func(q *DtNavMeshQuery, ref DtPolyRef, f *DtQueryFilter) DtStatus {
			var refs, parents [8]DtPolyRef
			var costs [8]float32
			var count int
			return q.FindPolysAroundShape(ref, []float32{0, 0, 0, 0, 0, 2, 8, 0, 2, 8, 0, 0}, 4, f, refs[:], parents[:], costs[:], &count, len(refs))
		},
		"wall": func(q *DtNavMeshQuery, ref DtPolyRef, f *DtQueryFilter) DtStatus {
			var distance float32
			var position, normal [3]float32
			return q.FindDistanceToWall(ref, []float32{1, 0, 1}, 10, f, &distance, position[:], normal[:])
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, q, ref := queryForTile(t, chainTile(t, 4, false, false))
			if st := run(q, ref, DtAllocDtQueryFilter()); DtStatusFailed(st) {
				t.Fatalf("lazy search failed: %#x", st)
			}
			if q.m_nodePool == nil || q.m_openList == nil || q.m_tinyNodePool != nil {
				t.Fatal("wrong scratch pools allocated")
			}
		})
	}
	t.Run("neighbourhood", func(t *testing.T) {
		_, q, ref := queryForTile(t, chainTile(t, 4, false, false))
		var refs, parents [8]DtPolyRef
		var count int
		if st := q.FindLocalNeighbourhood(ref, []float32{1, 0, 1}, 1, DtAllocDtQueryFilter(), refs[:], parents[:], &count, len(refs)); DtStatusFailed(st) {
			t.Fatalf("lazy local search failed: %#x", st)
		}
		if q.m_tinyNodePool == nil || q.m_nodePool != nil || q.m_openList != nil {
			t.Fatal("local neighbourhood allocated full search pools")
		}
	})
}

func TestRaycastScratchReuseDoesNotRetainVisitedState(t *testing.T) {
	_, q, ref := queryForTile(t, chainTile(t, 20, false, false))
	f := DtAllocDtQueryFilter()
	var path [24]DtPolyRef
	for _, endX := range []float32{39, 3, 39, 1} {
		hit := DtRaycastHit{Path: path[:], MaxPath: int32(len(path))}
		st := q.Raycast2(ref, []float32{1, 0, 1}, []float32{endX, 0, 1}, f, 0, &hit, 0)
		if DtStatusFailed(st) || DtStatusDetail(st, DT_OUT_OF_NODES) || hit.LastRef == 0 {
			t.Fatalf("raycast retained cycle state: %#x, %+v", st, hit)
		}
	}
	if q.m_nodePool != nil || q.m_openList != nil || q.m_tinyNodePool != nil {
		t.Fatal("raycast allocated search pools")
	}
}
