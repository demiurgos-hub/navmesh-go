package dtcache

import "github.com/demiurgos-hub/navmesh-go/detour"

func (c *DtTileCache) checkObstacleBounds(min, max []float32) detour.DtStatus {
	if c.m_tcomp == nil {
		return detour.DT_FAILURE | detour.DT_INVALID_PARAM
	}
	var refs [DT_MAX_TOUCHED_TILES]DtCompressedTileRef
	var n int32
	s := c.QueryTiles(min, max, refs[:], &n, DT_MAX_TOUCHED_TILES)
	if detour.DtStatusDetail(s, detour.DT_BUFFER_TOO_SMALL) {
		return detour.DT_FAILURE | detour.DT_BUFFER_TOO_SMALL
	}
	return s
}

func (c *DtTileCache) finishObstacle(ob *DtTileCacheObstacle) {
	if ob.State == DT_OBSTACLE_PROCESSING {
		ob.State = DT_OBSTACLE_PROCESSED
		return
	}
	if ob.State != DT_OBSTACLE_REMOVING {
		return
	}
	ob.State = DT_OBSTACLE_EMPTY
	ob.Salt++
	if ob.Salt == 0 {
		ob.Salt = 1
	}
	ob.Next = c.m_nextFreeObstacle
	c.m_nextFreeObstacle = ob
}

// Requests that do not fit the current update batch stay queued. Failed builds
// keep the head tile and pending obstacle state intact for the next Update.
func (c *DtTileCache) processObstacleRequests() detour.DtStatus {
	for c.m_nreqs > 0 {
		req := c.m_reqs[0]
		ob := c.GetObstacleByRef(req.ref)
		if ob != nil && ob.State != DT_OBSTACLE_EMPTY {
			refs := ob.Touched
			count := int32(ob.Ntouched)
			if req.action == REQUEST_ADD {
				var min, max [3]float32
				c.GetObstacleBounds(ob, min[:], max[:])
				s := c.QueryTiles(min[:], max[:], refs[:], &count, DT_MAX_TOUCHED_TILES)
				if detour.DtStatusFailed(s) || detour.DtStatusDetail(s, detour.DT_BUFFER_TOO_SMALL) {
					return detour.DT_FAILURE | (s & detour.DT_STATUS_DETAIL_MASK)
				}
			}
			needed := int32(0)
			for i := int32(0); i < count; i++ {
				if !contains(c.m_update[:], c.m_nupdate, refs[i]) {
					needed++
				}
			}
			if needed > MAX_UPDATE-c.m_nupdate {
				break
			}
			if req.action == REQUEST_ADD {
				ob.Touched = refs
				ob.Ntouched = uint8(count)
			} else {
				ob.State = DT_OBSTACLE_REMOVING
			}
			ob.Npending = uint8(count)
			ob.Pending = refs
			for i := int32(0); i < count; i++ {
				if !contains(c.m_update[:], c.m_nupdate, refs[i]) {
					c.m_update[c.m_nupdate] = refs[i]
					c.m_nupdate++
				}
			}
			if count == 0 {
				c.finishObstacle(ob)
			}
		}
		copy(c.m_reqs[:], c.m_reqs[1:c.m_nreqs])
		c.m_nreqs--
	}
	return detour.DT_SUCCESS
}
