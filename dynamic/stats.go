package dynamic

// RetentionStats describes managed snapshot ownership without copying geometry.
// RetiredGenerations excludes the current generation and an unpublished build.
type RetentionStats struct {
	CommittedRevision, DesiredRevision uint64
	Leases, RetiredGenerations         int
	Building, Prepared                 bool
}

// RetentionStats returns coherent lease and build counters under the world lock.
func (w *World) RetentionStats() RetentionStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := RetentionStats{CommittedRevision: w.current.Revision(), DesiredRevision: w.revision, Building: w.building, Prepared: w.prepared != nil}
	for snapshot, count := range w.leases {
		s.Leases += count
		if snapshot != w.current && count > 0 {
			s.RetiredGenerations++
		}
	}
	return s
}
