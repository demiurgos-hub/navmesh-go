package detour

// DT_MAX_LOCAL_NODES bounds the fixed node pool used by local surface queries.
const DT_MAX_LOCAL_NODES = 64

func (q *DtNavMeshQuery) ready() bool {
	return q != nil && q.m_nav != nil && q.m_nav.m_maxTiles > 0 && q.m_maxNodes > 0
}

func (q *DtNavMeshQuery) ensureSearchScratch() bool {
	if !q.ready() {
		return false
	}
	if q.m_nodePool == nil {
		q.m_nodePool = DtAllocNodePool(uint32(q.m_maxNodes), DtMaxUInt32(1, DtNextPow2(uint32(q.m_maxNodes/4))))
		q.m_openList = DtAllocNodeQueue(q.m_maxNodes)
	}
	return true
}

func (q *DtNavMeshQuery) ensureTinyScratch() bool {
	if !q.ready() {
		return false
	}
	if q.m_tinyNodePool == nil {
		q.m_tinyNodePool = DtAllocNodePool(DT_MAX_LOCAL_NODES, 32)
	}
	return true
}

func validExtents(v []float32) bool { return finite3(v) && v[0] >= 0 && v[1] >= 0 && v[2] >= 0 }

func validFilter(f *DtQueryFilter) bool {
	if f == nil {
		return false
	}
	for _, c := range f.m_areaCost {
		if !finite(c) || c < 0 {
			return false
		}
	}
	return true
}
