package debugger

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"strings"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// PolygonView is copied debug data, never a mutable runtime reference.
type PolygonView struct {
	Tile     artifact.TileKey
	Revision uint64
	Area     uint8
	Flags    uint16
	Vertices []geom.Vec3
}

// SnapshotReport is a portable debug capture for geometry, floor choice,
// corridors and pending updates. Callers can add their query probes as Points.
type SnapshotReport struct {
	World       string
	Revision    uint64
	Profile     geom.Profile
	Bounds      geom.Bounds
	Polygons    []PolygonView
	Links       []navmesh.LinkState
	Route       []navmesh.Corner
	Points      []geom.Vec3
	Bake        []bake.TileDiagnostic
	Dirty       []artifact.TileKey
	UpdateState dynamic.State
	UpdateError string
}

// Capture copies an immutable snapshot and optional path/update status. It
// starts no HTTP service and holds no query or mesh state after returning.
func Capture(s *navmesh.Snapshot, route *navmesh.Path, update *dynamic.Update) (SnapshotReport, error) {
	var report SnapshotReport
	if s == nil {
		return report, fmt.Errorf("debugger: nil snapshot")
	}
	b, err := s.Bundle()
	if err != nil {
		return report, err
	}
	report.World, report.Revision, report.Profile, report.Bounds = b.WorldID, b.Revision, b.Profile, b.Settings.Bounds
	report.Links = s.LinkStates()
	report.Route = route.Corners()
	if update != nil {
		report.Dirty = append([]artifact.TileKey(nil), update.Dirty...)
		report.UpdateState = update.State
		if update.Err != nil {
			report.UpdateError = update.Err.Error()
		}
	}
	for _, tile := range b.Tiles {
		if tile.Empty {
			continue
		}
		p := tile.Parameters
		for i := 0; i < int(p.PolyCount); i++ {
			v := PolygonView{Tile: tile.Key, Revision: tile.Revision, Area: p.PolyAreas[i], Flags: p.PolyFlags[i]}
			for j := 0; j < int(p.Nvp); j++ {
				idx := p.Polys[i*int(p.Nvp)*2+j]
				if idx == 0xffff {
					break
				}
				k := int(idx) * 3
				v.Vertices = append(v.Vertices, geom.Vec3{X: b.Settings.Origin.X + float64(p.Bmin[0]) + float64(p.Verts[k])*float64(p.Cs), Y: b.Settings.Origin.Y + float64(p.Bmin[1]) + float64(p.Verts[k+1])*float64(p.Ch), Z: b.Settings.Origin.Z + float64(p.Bmin[2]) + float64(p.Verts[k+2])*float64(p.Cs)})
			}
			report.Polygons = append(report.Polygons, v)
		}
	}
	return report, nil
}

// WriteJSON writes a reproducible structural capture (timings may vary).
func (r SnapshotReport) WriteJSON(w io.Writer) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(r)
}

// WriteSVG renders top (XZ) or side (XY) geometry, route/link markers, and query
// points. Side views reveal stacked-floor choices hidden by a top-down view.
func (r SnapshotReport) WriteSVG(w io.Writer, side bool) error {
	if !r.Bounds.Valid() {
		return fmt.Errorf("debugger: invalid bounds")
	}
	minX, maxX, minY, maxY := r.Bounds.Min.X, r.Bounds.Max.X, r.Bounds.Min.Z, r.Bounds.Max.Z
	if side {
		minY, maxY = r.Bounds.Min.Y, r.Bounds.Max.Y
	}
	dx, dy := math.Max(.001, maxX-minX), math.Max(.001, maxY-minY)
	project := func(p geom.Vec3) (float64, float64) {
		y := p.Z
		if side {
			y = p.Y
		}
		return 20 + (p.X-minX)/dx*960, 620 - (y-minY)/dy*580
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 660"><rect width="1000" height="660" fill="#111827"/><text x="20" y="24" fill="white">%s · revision %d · %s</text>`, html.EscapeString(r.World), r.Revision, html.EscapeString(r.Profile.ID))
	for _, p := range r.Polygons {
		var pts strings.Builder
		for _, v := range p.Vertices {
			x, y := project(v)
			fmt.Fprintf(&pts, "%.3f,%.3f ", x, y)
		}
		fmt.Fprintf(&out, `<polygon points="%s" fill="hsl(%d,55%%,45%%)" fill-opacity=".35" stroke="#94a3b8" stroke-width=".8"><title>tile %v revision %d area %d</title></polygon>`, pts.String(), int(p.Area)*47%360, p.Tile, p.Revision, p.Area)
	}
	for _, l := range r.Links {
		x, y := project(l.Start)
		a, b := project(l.End)
		color := "#fb7185"
		if l.Available {
			color = "#c084fc"
		}
		fmt.Fprintf(&out, `<line x1="%.3f" y1="%.3f" x2="%.3f" y2="%.3f" stroke="%s" stroke-width="3"><title>%s %s</title></line>`, x, y, a, b, color, html.EscapeString(l.ID), html.EscapeString(l.Reason))
	}
	for i, c := range r.Route {
		x, y := project(c.Position)
		if i > 0 {
			a, b := project(r.Route[i-1].Position)
			fmt.Fprintf(&out, `<line x1="%.3f" y1="%.3f" x2="%.3f" y2="%.3f" stroke="#facc15" stroke-width="3"/>`, a, b, x, y)
		}
		fmt.Fprintf(&out, `<circle cx="%.3f" cy="%.3f" r="4" fill="#facc15"/>`, x, y)
	}
	for _, p := range r.Points {
		x, y := project(p)
		fmt.Fprintf(&out, `<circle cx="%.3f" cy="%.3f" r="5" fill="#38bdf8"/>`, x, y)
	}
	out.WriteString("</svg>")
	_, err := io.WriteString(w, out.String())
	return err
}
