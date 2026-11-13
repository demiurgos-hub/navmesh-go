package detour

import (
	"strings"
	"testing"
)

func TestCreateParameterDiagnosticsIdentifyInvalidSection(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		edit       func(*DtNavMeshCreateParams)
	}{
		{"counts", "invalid tile counts", func(p *DtNavMeshCreateParams) { p.VertCount = 0 }},
		{"array", "truncated polygon or vertex arrays", func(p *DtNavMeshCreateParams) { p.Verts = nil }},
		{"portal", "polygon 0 edge 0 has invalid portal", func(p *DtNavMeshCreateParams) { p.Polys[4] = 0x8004 }},
		{"neighbor", "polygon 0 edge 2 has invalid neighbor 0", func(p *DtNavMeshCreateParams) { p.Polys[6] = 0 }},
		{"convexity", "polygon 0", func(p *DtNavMeshCreateParams) { p.Verts[6], p.Verts[8] = 0, 1 }},
		{"detail", "polygon 0 has invalid detail range", func(p *DtNavMeshCreateParams) { p.DetailMeshes = make([]uint32, 8) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := graphParams()
			tc.edit(&p)
			err := ValidateCreateParams(&p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want diagnostic containing %q", err, tc.want)
			}
			if validCreateParams(&p) {
				t.Fatal("boolean admission disagrees")
			}
		})
	}
}
