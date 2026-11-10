// Package testworld supplies a deterministic server-only geometry fixture.
package testworld

import (
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// Plane creates an upward-wound rectangle as an independent geometry asset.
func Plane(id string, min, max geom.Vec3) geom.Asset {
	return geom.Asset{ID: id, Vertices: []geom.Vec3{{X: min.X, Y: min.Y, Z: min.Z}, {X: min.X, Y: min.Y, Z: max.Z}, {X: max.X, Y: max.Y, Z: max.Z}, {X: max.X, Y: max.Y, Z: min.Z}}, Triangles: []geom.Triangle{{Indices: [3]uint32{0, 1, 2}, Area: 1}, {Indices: [3]uint32{0, 2, 3}, Area: 1}}}
}

// Scene contains a ground floor and a separately removable bridge above it.
func Scene() (geom.Scene, bake.Config) {
	ground := Plane("ground", geom.Vec3{X: -8, Z: -8}, geom.Vec3{X: 8, Z: 8})
	bridge := Plane("bridge", geom.Vec3{X: -6, Y: 3, Z: -2}, geom.Vec3{X: 6, Y: 3, Z: 2})
	s := geom.Scene{Assets: []geom.Asset{ground, bridge}, Instances: []geom.Instance{{ID: "terrain", AssetID: "ground", Revision: 1, Transform: geom.Identity()}, {ID: "bridge-instance", AssetID: "bridge", Revision: 1, Transform: geom.Identity()}}}
	c := bake.DefaultConfig("fixture", geom.Bounds{Min: geom.Vec3{X: -8, Y: -1, Z: -8}, Max: geom.Vec3{X: 8, Y: 8, Z: 8}})
	c.Settings.TileSize = 32
	return s, c
}
