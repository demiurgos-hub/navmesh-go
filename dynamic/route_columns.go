package dynamic

import (
	"math"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
)

func (s *Safety) buildRouteColumns(config bake.Config) {
	if len(s.bounds) == 0 {
		return
	}
	settings := config.Settings
	// The validated bake grid is fixed for this world. Search its exact floating
	// boundary expressions, avoiding inverse-coordinate rounding at closed edges
	// and cancellation from large translated origins.
	x0 := int64(math.Floor((settings.Bounds.Min.X - s.origin.X) / s.tileWidth))
	x1 := int64(math.Ceil((settings.Bounds.Max.X - s.origin.X) / s.tileWidth))
	z0 := int64(math.Floor((settings.Bounds.Min.Z - s.origin.Z) / s.tileWidth))
	z1 := int64(math.Ceil((settings.Bounds.Max.Z - s.origin.Z) / s.tileWidth))
	s.routeColumns = make(map[artifact.TileKey]bool, len(s.columns))
	for key := range s.columns {
		s.routeColumns[key] = true
	}
	for _, box := range s.bounds {
		minX, maxX := intersectingColumns(s.origin.X, s.tileWidth, box.Min.X, box.Max.X, x0, x1)
		minZ, maxZ := intersectingColumns(s.origin.Z, s.tileWidth, box.Min.Z, box.Max.Z, z0, z1)
		for z := minZ; z < maxZ; z++ {
			for x := minX; x < maxX; x++ {
				s.routeColumns[artifact.TileKey{X: int32(x), Z: int32(z)}] = true
			}
		}
	}
}

func intersectingColumns(origin, width, min, max float64, start, end int64) (int64, int64) {
	lo, hi := start, end
	for lo < hi {
		mid := lo + (hi-lo)/2
		if origin+(float64(mid)+1)*width >= min {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	first := lo
	lo, hi = start, end
	for lo < hi {
		mid := lo + (hi-lo)/2
		if origin+float64(mid)*width > max {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return first, lo
}
