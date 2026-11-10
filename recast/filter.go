// Modified Go translation for navmesh-go; see PROVENANCE.md.
// Copyright (c) 2009 Mikko Mononen memon@inside.org
//
// This software is provided 'as-is', without any express or implied
// warranty.  In no event will the authors be held liable for any damages
// arising from the use of this software.
//
// Permission is granted to anyone to use this software for any purpose,
// including commercial applications, and to alter it and redistribute it
// freely, subject to the following restrictions:
//
// 1. The origin of this software must not be misrepresented; you must not
// claim that you wrote the original software. If you use this software
// in a product, an acknowledgment in the product documentation would be
// appreciated but is not required.
// 2. Altered source versions must be plainly marked as such, and must not be
// misrepresented as being the original software.
// 3. This notice may not be removed or altered from any source distribution.
//

// Package recast implements navigation mesh generation.
package recast

import (
	"context"
)

// FilterLowHangingWalkableObstacles marks non-walkable spans as walkable if their maximum
// is within walkableClimb of the span below them.
func FilterLowHangingWalkableObstacles(ctx context.Context, walkableClimb int, hf *Heightfield) error {
	defer ScopedTimer(ctx, TimerFilterLowObstacles)()

	xSize := hf.Width
	zSize := hf.Height

	for z := 0; z < zSize; z++ {
		for x := 0; x < xSize; x++ {
			var previousSpan *Span
			previousWasWalkable := false
			previousAreaID := uint8(NullArea)

			// For each span in the column...
			for span := hf.Spans[x+z*xSize]; span != nil; previousSpan, span = span, span.Next {
				walkable := span.Area != NullArea

				// If current span is not walkable, but there is walkable span just below it and the height difference
				// is small enough for the agent to walk over, mark the current span as walkable too.
				if !walkable && previousWasWalkable && int(span.Smax)-int(previousSpan.Smax) <= walkableClimb {
					span.Area = uint32(previousAreaID)
				}

				// Copy the original walkable value regardless of whether we changed it.
				// This prevents multiple consecutive non-walkable spans from being erroneously marked as walkable.
				previousWasWalkable = walkable
				previousAreaID = uint8(span.Area)
			}
		}
	}
	return nil
}

// FilterLedgeSpans marks spans that are ledges as not-walkable.
func FilterLedgeSpans(ctx context.Context, walkableHeight, walkableClimb int, hf *Heightfield) error {
	defer ScopedTimer(ctx, TimerFilterBorder)()

	xSize := hf.Width
	zSize := hf.Height

	// Mark spans that are adjacent to a ledge as unwalkable.
	for z := 0; z < zSize; z++ {
		for x := 0; x < xSize; x++ {
			for span := hf.Spans[x+z*xSize]; span != nil; span = span.Next {
				// Skip non-walkable spans.
				if span.Area == NullArea {
					continue
				}

				floor := int(span.Smax)
				ceiling := MaxHeightfieldHeight
				if span.Next != nil {
					ceiling = int(span.Next.Smin)
				}

				// The difference between this walkable area and the lowest neighbor walkable area.
				lowestNeighborFloorDifference := MaxHeightfieldHeight

				// Min and max height of accessible neighbours.
				lowestTraversableNeighborFloor := int(span.Smax)
				highestTraversableNeighborFloor := int(span.Smax)

				for direction := 0; direction < 4; direction++ {
					neighborX := x + DirOffsetX(direction)
					neighborZ := z + DirOffsetZ(direction)

					// Skip neighbours which are out of bounds.
					if neighborX < 0 || neighborZ < 0 || neighborX >= xSize || neighborZ >= zSize {
						lowestNeighborFloorDifference = -walkableClimb - 1
						break
					}

					neighborSpan := hf.Spans[neighborX+neighborZ*xSize]

					// The most we can step down to the neighbor is the walkableClimb distance.
					// Start with the area under the neighbor span
					neighborCeiling := MaxHeightfieldHeight
					if neighborSpan != nil {
						neighborCeiling = int(neighborSpan.Smin)
					}

					// Skip neighbour if the gap between the spans is too small.
					if Min(ceiling, neighborCeiling)-floor >= walkableHeight {
						lowestNeighborFloorDifference = -walkableClimb - 1
						break
					}

					// For each span in the neighboring column...
					for ; neighborSpan != nil; neighborSpan = neighborSpan.Next {
						neighborFloor := int(neighborSpan.Smax)
						neighborCeiling = MaxHeightfieldHeight
						if neighborSpan.Next != nil {
							neighborCeiling = int(neighborSpan.Next.Smin)
						}

						// Only consider neighboring areas that have enough overlap to be potentially traversable.
						if Min(ceiling, neighborCeiling)-Max(floor, neighborFloor) < walkableHeight {
							continue
						}

						neighborFloorDifference := neighborFloor - floor
						lowestNeighborFloorDifference = Min(lowestNeighborFloorDifference, neighborFloorDifference)

						// Find min/max accessible neighbor height.
						// Only consider neighbors that are at most walkableClimb away.
						if Abs(neighborFloorDifference) <= walkableClimb {
							lowestTraversableNeighborFloor = Min(lowestTraversableNeighborFloor, neighborFloor)
							highestTraversableNeighborFloor = Max(highestTraversableNeighborFloor, neighborFloor)
						} else if neighborFloorDifference < -walkableClimb {
							break
						}
					}
				}

				// The current span is close to a ledge if the magnitude of the drop to any neighbour span
				// is greater than the walkableClimb distance.
				if lowestNeighborFloorDifference < -walkableClimb {
					span.Area = NullArea
				} else if highestTraversableNeighborFloor-lowestTraversableNeighborFloor > walkableClimb {
					span.Area = NullArea
				}
			}
		}
	}
	return nil
}

// FilterWalkableLowHeightSpans marks walkable spans as not walkable if the clearance above
// the span is less than the specified walkableHeight.
func FilterWalkableLowHeightSpans(ctx context.Context, walkableHeight int, hf *Heightfield) error {
	defer ScopedTimer(ctx, TimerFilterWalkable)()

	xSize := hf.Width
	zSize := hf.Height

	// Remove walkable flag from spans which do not have enough
	// space above them for the agent to stand there.
	for z := 0; z < zSize; z++ {
		for x := 0; x < xSize; x++ {
			for span := hf.Spans[x+z*xSize]; span != nil; span = span.Next {
				floor := int(span.Smax)
				ceiling := MaxHeightfieldHeight
				if span.Next != nil {
					ceiling = int(span.Next.Smin)
				}
				if ceiling-floor < walkableHeight {
					span.Area = NullArea
				}
			}
		}
	}
	return nil
}
