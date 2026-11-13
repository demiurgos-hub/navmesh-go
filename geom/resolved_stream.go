package geom

// The streamed form hashes the same canonical bytes as fingerprintResolved,
// including null for an empty triangle array, without retaining that array.
type resolvedFingerprintStream struct {
	writer  *fingerprintWriter
	started bool
}

func (s *resolvedFingerprintStream) triangle(triangle *ResolvedTriangle) {
	if !s.started {
		s.writer.raw(`{"Triangles":[`)
		s.started = true
	} else {
		s.writer.raw(",")
	}
	s.writer.value(triangle)
}
func (s *resolvedFingerprintStream) finish(metadata Resolved) (string, error) {
	if s.started {
		s.writer.raw("]")
	} else {
		s.writer.raw(`{"Triangles":null`)
	}
	s.writer.raw(`,"Volumes":`)
	fingerprintArray(s.writer, metadata.Volumes)
	s.writer.raw(`,"Links":`)
	fingerprintArray(s.writer, metadata.Links)
	s.writer.raw("}")
	return s.writer.result()
}
