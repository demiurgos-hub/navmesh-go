package artifact

import (
	"fmt"
	"math"

	"github.com/demiurgos-hub/navmesh-go/geom"
)

// ClearancePrecision selects arithmetic only for profile-to-voxel ratios.
// It never changes source coordinates, transforms, grid bounds or geometry.
type ClearancePrecision string

const (
	// ClearanceBinary64 preserves the default float64 division before rounding.
	ClearanceBinary64 ClearancePrecision = ""
	// ClearanceBinary32 rounds operands and the quotient to float32 before rounding.
	// Use it for profiles and cell dimensions authored as native float32 values.
	ClearanceBinary32 ClearancePrecision = "binary32"
)

// VoxelClearance contains conservative integral clearance and climb limits.
type VoxelClearance struct{ Height, Radius, Climb int }

// QuantizeClearance rounds height/radius up and climb down in the explicitly
// selected precision. It rejects invalid modes, unrepresentable positive
// dimensions, nonfinite ratios and counts outside native voxel capacities.
func (s Settings) QuantizeClearance(profile geom.Profile) (VoxelClearance, error) {
	var result VoxelClearance
	if s.ClearancePrecision != ClearanceBinary64 && s.ClearancePrecision != ClearanceBinary32 {
		return result, fmt.Errorf("artifact: unsupported clearance precision %q", s.ClearancePrecision)
	}
	ratio := func(value, cell float64, positiveValue bool) (float64, error) {
		if !nonnegative(value) || positiveValue && value == 0 || !positive(cell) {
			return 0, fmt.Errorf("artifact: invalid clearance dimension")
		}
		quotient := value / cell
		if s.ClearancePrecision == ClearanceBinary32 {
			numerator, denominator := float32(value), float32(cell)
			if !nonnegative(float64(numerator)) || value > 0 && numerator == 0 || !positive(float64(denominator)) {
				return 0, fmt.Errorf("artifact: clearance dimension not representable in binary32")
			}
			quotient = float64(float32(numerator / denominator))
		}
		if !nonnegative(quotient) {
			return 0, fmt.Errorf("artifact: nonfinite clearance ratio")
		}
		return quotient, nil
	}
	height, err := ratio(profile.Height, s.CellHeight, true)
	if err != nil {
		return result, err
	}
	radius, err := ratio(profile.Radius, s.CellSize, false)
	if err != nil {
		return result, err
	}
	climb, err := ratio(profile.MaxClimb, s.CellHeight, false)
	if err != nil {
		return result, err
	}
	height, radius, climb = math.Ceil(height), math.Ceil(radius), math.Floor(climb)
	if height < 1 || height > 255 || radius > 127 || climb >= 8191 {
		return result, fmt.Errorf("artifact: voxel clearance capacity exceeded")
	}
	return VoxelClearance{Height: int(height), Radius: int(radius), Climb: int(climb)}, nil
}
