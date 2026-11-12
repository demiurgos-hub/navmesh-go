package dynamic

import (
	"math"
	"math/rand"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func TestRouteColumnCacheMatchesClosedBounds(t *testing.T) {
	random := rand.New(rand.NewSource(801))
	for _, origin := range []float64{-1e15, -1234.567, 0, 1234.567, 1e15} {
		for _, width := range []float64{.2, 1, 3.2, 16} {
			for _, radius := range []float64{0, .4, 9} {
				cfg := bake.DefaultConfig("cache", geom.Bounds{Min: geom.Vec3{X: origin - 16*width, Y: -1, Z: origin - 16*width}, Max: geom.Vec3{X: origin + 16*width, Y: 1, Z: origin + 16*width}})
				cfg.Settings.Origin = geom.Vec3{X: origin, Z: origin}
				cfg.Settings.TileSize, cfg.Settings.CellSize, cfg.Profile.Radius = 1, width, radius
				s := &Safety{origin: cfg.Settings.Origin, tileWidth: width, columns: map[artifact.TileKey]bool{}}
				for i := 0; i < 3; i++ {
					s.columns[artifact.TileKey{X: int32(random.Intn(24) - 12), Z: int32(random.Intn(24) - 12)}] = true
				}
				s.buildBounds(cfg)
				for z := int32(-16); z < 16; z++ {
					for x := int32(-16); x < 16; x++ {
						min := geom.Vec3{X: origin + float64(x)*width, Z: origin + float64(z)*width}
						max := geom.Vec3{X: origin + (float64(x)+1)*width, Z: origin + (float64(z)+1)*width}
						key := artifact.TileKey{X: x, Z: z}
						want := s.columns[key] || !s.SafeBounds(min, max)
						if s.routeColumns[key] != want {
							t.Fatalf("origin=%g width=%g radius=%g tile=%v cached=%v expected=%v", origin, width, radius, key, s.routeColumns[key], want)
						}
					}
				}
			}
		}
	}
}

func TestIntersectingColumnsKeepsBoundaryTouches(t *testing.T) {
	for _, boundary := range []float64{-8, -1, 0, 1, 8} {
		for _, point := range []float64{math.Nextafter(boundary, math.Inf(-1)), boundary, math.Nextafter(boundary, math.Inf(1))} {
			first, end := intersectingColumns(0, 1, point, point, -16, 16)
			for key := int64(-16); key < 16; key++ {
				want := float64(key) <= point && float64(key)+1 >= point
				if got := key >= first && key < end; got != want {
					t.Fatalf("point=%g tile=%d range=[%d,%d) got=%v expected=%v", point, key, first, end, got, want)
				}
			}
		}
	}
}
