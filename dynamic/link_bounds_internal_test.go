package dynamic

import (
	"math"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/geom"
)

func TestSourceLinkBoundsPreserveXZAndBoundHeight(t *testing.T) {
	region := geom.Bounds{Min: geom.Vec3{X: -8, Y: -2, Z: -8}, Max: geom.Vec3{X: 8, Y: 6, Z: 8}}
	link := geom.Link{Start: geom.Vec3{X: -4, Y: 100, Z: 2}, End: geom.Vec3{X: 6, Y: 200, Z: -3}, Radius: .5}
	want := geom.Bounds{Min: geom.Vec3{X: -4.5, Y: -2, Z: -3.5}, Max: geom.Vec3{X: 6.5, Y: 6, Z: 2.5}}
	if got := sourceLinkBounds(link, region); got != want {
		t.Fatalf("link influence = %+v, want %+v", got, want)
	}
	link.Start = geom.Vec3{X: -math.MaxFloat64, Y: -math.MaxFloat64, Z: -math.MaxFloat64}
	link.End = geom.Vec3{X: math.MaxFloat64, Y: math.MaxFloat64, Z: math.MaxFloat64}
	link.Radius = math.MaxFloat64
	got := sourceLinkBounds(link, region)
	if !got.Valid() || got.Min.Y != region.Min.Y || got.Max.Y != region.Max.Y || got.Min.X != -math.MaxFloat64 || got.Max.X != math.MaxFloat64 || got.Min.Z != -math.MaxFloat64 || got.Max.Z != math.MaxFloat64 {
		t.Fatal("finite link influence overflowed or lost coverage", got)
	}
}
