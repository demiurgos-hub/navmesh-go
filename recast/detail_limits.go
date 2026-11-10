package recast

import (
	"context"
	"math"
)

const defaultMaxDetailSamples = 1 << 20

// The square spiral uses (2*r+1)^2-1 iterations. Keep that expression in
// signed 32-bit range so the same settings are safe on every supported Go int.
const maxDetailHeightSearchRadius = 23169

type detailSamplingKey struct{}
type detailSamplingBudget struct {
	remaining int
	exceeded  bool
}

func newDetailSamplingBudget(limit int) *detailSamplingBudget {
	if limit == 0 {
		limit = defaultMaxDetailSamples
	}
	return &detailSamplingBudget{remaining: limit}
}

// detailSampleGrid checks the complete candidate rectangle before allocating
// samples, converting coordinates to int, or entering a sampling loop. Charge
// overlapping polygon rectangles separately: each represents actual work.
func detailSampleGrid(ctx context.Context, bmin, bmax [3]float32, spacing float32) (x0, x1, z0, z1 int, ok bool) {
	budget, _ := ctx.Value(detailSamplingKey{}).(*detailSamplingBudget)
	if budget == nil {
		budget = newDetailSamplingBudget(0)
	}
	reject := func() (int, int, int, int, bool) {
		budget.exceeded = true
		reportError(ctx, "detail sampling grid exceeds coordinate or candidate limit")
		return 0, 0, 0, 0, false
	}
	// Retain the reference's float32 division when representable. Inspect its
	// result in float64 before narrowing so overflow never reaches an int.
	xlo, xhi := math.Floor(float64(bmin[0]/spacing)), math.Ceil(float64(bmax[0]/spacing))
	zlo, zhi := math.Floor(float64(bmin[2]/spacing)), math.Ceil(float64(bmax[2]/spacing))
	intLimit := float64(int(^uint(0) >> 1))
	for _, coordinate := range []float64{xlo, xhi, zlo, zhi} {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || coordinate <= -intLimit || coordinate >= intLimit {
			return reject()
		}
	}
	width, height := xhi-xlo, zhi-zlo
	// Four ints are stored per accepted candidate. The rectangle is a safe
	// upper bound even if all candidates lie inside the polygon.
	limit := budget.remaining
	if sliceLimit := int(^uint(0)>>1) / 4; limit > sliceLimit {
		limit = sliceLimit
	}
	if width < 0 || height < 0 || limit < 0 || (height > 0 && width > float64(limit)/height) {
		return reject()
	}
	budget.remaining -= int(width * height)
	return int(xlo), int(xhi), int(zlo), int(zhi), true
}
