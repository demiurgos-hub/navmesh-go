package artifact

import (
	"fmt"
	"math"

	"github.com/demiurgos-hub/navmesh-go/geom"
)

func validateGrid(s Settings, p geom.Profile) ([4]float64, error) {
	var grid [4]float64
	tw := s.CellSize * float64(s.TileSize)
	if !positive(float64(float32(s.CellSize))) || !positive(float64(float32(s.CellHeight))) || !positive(float64(float32(tw))) {
		return grid, fmt.Errorf("artifact: grid not representable in float32")
	}
	grid = [4]float64{math.Floor((s.Bounds.Min.X - s.Origin.X) / tw), math.Ceil((s.Bounds.Max.X - s.Origin.X) / tw), math.Floor((s.Bounds.Min.Z - s.Origin.Z) / tw), math.Ceil((s.Bounds.Max.Z - s.Origin.Z) / tw)}
	if !(grid[0] >= math.MinInt32 && grid[1] <= math.MaxInt32 && grid[2] >= math.MinInt32 && grid[3] <= math.MaxInt32) || grid[0] >= grid[1] || grid[2] >= grid[3] || (grid[1]-grid[0])*(grid[3]-grid[2]) > 65536 {
		return grid, fmt.Errorf("artifact: region tile coordinate/count limit")
	}
	if s.Bounds.Min.Y >= s.Bounds.Max.Y || (s.Bounds.Max.Y-s.Bounds.Min.Y)/s.CellHeight > 8191 || math.Ceil(p.Height/s.CellHeight) > 255 || math.Ceil(p.Radius/s.CellSize) > 127 {
		return grid, fmt.Errorf("artifact: vertical/clearance capacity exceeded")
	}
	padding := (math.Ceil(p.Radius/s.CellSize) + 3) * s.CellSize
	values := []float64{grid[0]*tw - padding, grid[1]*tw + padding, grid[2]*tw - padding, grid[3]*tw + padding, s.Bounds.Min.Y - s.Origin.Y, s.Bounds.Max.Y - s.Origin.Y}
	limit := math.Min(s.CellSize, s.CellHeight) / 16
	for _, v := range values {
		f := float32(v)
		ulp := math.Abs(float64(math.Nextafter32(f, float32(math.Inf(1)))) - float64(f))
		if math.IsNaN(ulp) || math.IsInf(ulp, 0) || ulp > limit {
			return grid, fmt.Errorf("artifact: region exceeds local float32 precision envelope")
		}
	}
	return grid, nil
}

func gridAligned(actual float32, expected, resolution float64) bool {
	f := float32(expected)
	ulp := math.Abs(float64(math.Nextafter32(f, float32(math.Inf(1)))) - float64(f))
	return math.Abs(float64(actual)-expected) <= math.Max(resolution*1e-4, 8*ulp)
}

func tileAligned(s Settings, profile geom.Profile, k TileKey, min, max [3]float32, layer bool) bool {
	w := s.CellSize * float64(s.TileSize)
	if !gridAligned(min[0], float64(k.X)*w, s.CellSize) || !gridAligned(max[0], (float64(k.X)+1)*w, s.CellSize) || !gridAligned(min[2], float64(k.Z)*w, s.CellSize) || !gridAligned(max[2], (float64(k.Z)+1)*w, s.CellSize) {
		return false
	}
	if layer {
		return float64(min[1]) >= s.Bounds.Min.Y-s.Origin.Y-s.CellHeight && float64(max[1]) <= s.Bounds.Max.Y-s.Origin.Y+s.CellHeight
	}
	// Recast compact fields extend the upper bound by voxelized clearance.
	clearance := math.Ceil(profile.Height/s.CellHeight) * s.CellHeight
	return gridAligned(min[1], s.Bounds.Min.Y-s.Origin.Y, s.CellHeight) && gridAligned(max[1], s.Bounds.Max.Y-s.Origin.Y+clearance, s.CellHeight)
}
