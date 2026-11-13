package recast

import (
	"reflect"
	"testing"
)

func TestContourTopologyRefinementPreservesErrorAndEdgeLimits(t *testing.T) {
	points, _ := crossingWallContour(t)
	for _, maxEdge := range []int{0, 7, 70} {
		simplified := simplifyContour(points, 2, maxEdge, int(contourTessWallEdges|contourTessAreaEdges))
		indices := make([]int, len(simplified)/4)
		for i := range indices {
			indices[i] = -1
			for j := 0; j < len(points)/4; j++ {
				if reflect.DeepEqual(simplified[i*4:i*4+3], points[j*4:j*4+3]) {
					indices[i] = j
					break
				}
			}
			if indices[i] < 0 {
				t.Fatalf("edge limit %d invented a contour vertex: %v", maxEdge, simplified[i*4:i*4+3])
			}
		}
		for i, start := range indices {
			next := (i + 1) % len(indices)
			end := indices[next]
			a, b := simplified[i*4:], simplified[next*4:]
			for j := (start + 1) % (len(points) / 4); j != end; j = (j + 1) % (len(points) / 4) {
				distance := distancePtSegInt(points[j*4], points[j*4+2], a[0], a[2], b[0], b[2])
				if distance > 4 {
					t.Fatalf("edge limit %d: refinement exceeded maxError: edge %d raw point %d squared distance %g", maxEdge, i, j, distance)
				}
			}
			dx, dz := a[0]-b[0], a[2]-b[2]
			if maxEdge > 0 && dx*dx+dz*dz > maxEdge*maxEdge {
				t.Fatalf("refinement exceeded edge length %d: %v -> %v", maxEdge, a[:3], b[:3])
			}
		}
	}
}

func TestContourTopologyRefinementPreservesSharedPortalSubdivision(t *testing.T) {
	// A narrow U has two crossing simplified wall chords. Its left side is a
	// shared region portal with an omitted collinear sample; repairing the U
	// must preserve the exact two endpoint portal seen by its neighbor.
	points := []int{
		-4, 0, 0, 0, -4, 0, 2, 7, -4, 0, 4, 7,
		4, 0, 4, 0, 4, 0, 0, 0, 2, 0, 0, 0,
		2, 0, 2, 0, -2, 0, 2, 0, -2, 0, 0, 0,
	}
	prior := policyContourSelection(points, []int{0, 2, 4, 6, 8})
	got := refineCrossingWallEdges(points, append([]int(nil), prior...))
	if len(got) <= len(prior) {
		t.Fatal("crossing wall chords were not refined")
	}
	if !reflect.DeepEqual(got[:8], prior[:8]) {
		t.Fatal("shared portal endpoint subdivision changed")
	}
	for i := 0; i < len(got)/4; i++ {
		if got[i*4+3] == 1 {
			t.Fatal("topology repair introduced a T-junction on a shared portal")
		}
	}
	for i := 0; i < len(got)/4; i++ {
		ni := (i + 1) % (len(got) / 4)
		for j := i + 1; j < len(got)/4; j++ {
			nj := (j + 1) % (len(got) / 4)
			if j != ni && nj != i && intersectPropInt(got[i*4:], got[ni*4:], got[j*4:], got[nj*4:]) {
				t.Fatal("wall refinement left a proper crossing")
			}
		}
	}
}

func TestContourTopologyRefinementRetainsWeaklySimpleSharedEndpoints(t *testing.T) {
	// Two clockwise loops meet at one raw vertex. This intentional weakly
	// simple contact is not a newly crossing simplification chord.
	points := []int{
		0, 0, 0, 0, -1, 0, 0, 0, -2, 0, 0, 0, -2, 0, 2, 0, 0, 0, 2, 0,
		0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 2, 0, -2, 0, 0, 0, -2, 0,
	}
	prior := policyContourSelection(points, []int{0, 2, 3, 4, 5, 7, 8, 9})
	got := refineCrossingWallEdges(points, append([]int(nil), prior...))
	if !reflect.DeepEqual(got, prior) {
		t.Fatalf("shared endpoint topology changed: %v", got)
	}
}

func TestContourTopologyRefinementDoesNotRepairCrossingRawEdges(t *testing.T) {
	points := []int{0, 0, 0, 0, 4, 0, 4, 0, 0, 0, 4, 0, 4, 0, 0, 0}
	prior := policyContourSelection(points, []int{0, 1, 2, 3})
	got := refineCrossingWallEdges(points, append([]int(nil), prior...))
	if !reflect.DeepEqual(got, prior) {
		t.Fatal("invalid raw geometry was silently rewritten")
	}
}

func policyContourSelection(points, indices []int) []int {
	var selected []int
	for _, index := range indices {
		selected = append(selected, points[index*4], points[index*4+1], points[index*4+2], index)
	}
	return selected
}
