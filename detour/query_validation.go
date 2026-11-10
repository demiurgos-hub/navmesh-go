package detour

func (q *DtNavMeshQuery) ready() bool {
	return q != nil && q.m_nav != nil && q.m_nav.m_maxTiles > 0 && q.m_nodePool != nil && q.m_tinyNodePool != nil && q.m_openList != nil
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
