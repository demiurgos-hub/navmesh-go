package detour

// ReplaceTile validates the complete replacement before removing the old tile.
// It restores the previous tile and reference if adding the new tile fails.
// Like AddTile, this requires exclusive ownership of the mesh during the call.
func (m *DtNavMesh) ReplaceTile(data []byte, dataSize int, flags DtTileFlags, result *DtTileRef) DtStatus {
	if result != nil {
		*result = 0
	}
	if m == nil || dataSize != len(data) {
		return DT_FAILURE | DT_INVALID_PARAM
	}
	t, err := DecodeNavMeshData(data)
	if err != nil {
		return tileErrorStatus(err)
	}
	if uint32(t.Header.PolyCount) > m.m_params.MaxPolys {
		return DT_FAILURE | DT_INVALID_PARAM
	}
	old := m.GetTileAt(t.Header.X, t.Header.Y, t.Header.Layer)
	if old == nil {
		return m.AddTile(data, dataSize, flags, 0, result)
	}
	oldData, err := EncodeNavMeshData(old)
	if err != nil {
		return tileErrorStatus(err)
	}
	oldRef, oldFlags := m.GetTileRef(old), old.Flags
	if s := m.RemoveTile(oldRef, nil, nil); DtStatusFailed(s) {
		return s
	}
	s := m.AddTile(data, dataSize, flags, 0, result)
	if DtStatusFailed(s) {
		_ = m.AddTile(oldData, len(oldData), oldFlags, oldRef, nil)
	}
	return s
}
