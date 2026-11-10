package navmesh

import (
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// LinkState reports authoring identity and endpoint attachment after assembly.
// Endpoint coordinates are projected onto the actual connected detail surface.
// A host must recheck current link revision/availability before traversal begins.
type LinkState struct {
	ID         string
	Revision   uint64
	Available  bool
	Reason     string
	Start, End Vec3
}

// LinkStates returns copied diagnostics for every authored link. Unsupported
// long-distance links (outside the owner/neighbor tile attachment range) remain
// unavailable instead of pretending to provide connectivity.
func (s *Snapshot) LinkStates() []LinkState {
	if s == nil {
		return nil
	}
	states := make(map[uint32]LinkState, len(s.bundle.Links))
	for _, l := range s.bundle.Links {
		v := LinkState{ID: l.ID, Revision: l.Revision, Start: l.Start, End: l.End, Reason: "endpoint not attached"}
		if l.Disabled {
			v.Reason = "disabled"
		} else if len(l.Profiles) > 0 {
			eligible := false
			for _, p := range l.Profiles {
				eligible = eligible || p == s.bundle.Profile.ID
			}
			if !eligible {
				v.Reason = "profile excluded"
			}
		}
		states[geom.LinkKey(l.ID)] = v
	}
	o := s.bundle.Settings.Origin
	for i := 0; i < int(s.mesh.GetMaxTiles()); i++ {
		tile := s.mesh.GetTile(i)
		if tile == nil || tile.Header == nil {
			continue
		}
		for _, con := range tile.OffMeshCons {
			state, ok := states[con.UserId]
			if !ok || state.Reason != "endpoint not attached" {
				continue
			}
			poly := &tile.Polys[con.Poly]
			var attached [2]bool
			for j := poly.FirstLink; j != detour.DT_NULL_LINK; j = tile.Links[j].Next {
				link := tile.Links[j]
				if link.Ref != 0 && link.Edge < 2 {
					attached[link.Edge] = true
				}
			}
			if attached[0] && attached[1] {
				state.Available, state.Reason = true, ""
				a, b := int(poly.Verts[0])*3, int(poly.Verts[1])*3
				state.Start = Vec3{X: float64(tile.Verts[a]) + o.X, Y: float64(tile.Verts[a+1]) + o.Y, Z: float64(tile.Verts[a+2]) + o.Z}
				state.End = Vec3{X: float64(tile.Verts[b]) + o.X, Y: float64(tile.Verts[b+1]) + o.Y, Z: float64(tile.Verts[b+2]) + o.Z}
			}
			states[con.UserId] = state
		}
	}
	result := make([]LinkState, 0, len(states))
	for _, l := range s.bundle.Links {
		result = append(result, states[geom.LinkKey(l.ID)])
	}
	return result
}
