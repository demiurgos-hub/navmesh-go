package recast

import "testing"

func TestDiagonalRequiresInteriorAtBothBridgeEndpoints(t *testing.T) {
	// Clockwise outer square with one counterclockwise square hole joined by
	// duplicate bridge endpoints. A diagonal can enter one occurrence of a
	// bridge vertex through the wrong local wedge without crossing an edge.
	verts, indices := diagonalPolicyContour([][2]int{
		{0, 0}, {0, 10}, {10, 10}, {10, 0}, {0, 0},
		{4, 4}, {6, 4}, {6, 6}, {4, 6}, {4, 4},
	})
	n := len(indices)
	if !inConeInt(1, 5, n, verts, indices) || inConeInt(5, 1, n, verts, indices) || !diagonalie(1, 5, n, verts, indices) {
		t.Fatal("fixture no longer isolates the destination endpoint cone")
	}
	if !inConeLoose(1, 5, n, verts, indices) || inConeLoose(5, 1, n, verts, indices) || !diagonalieLoose(1, 5, n, verts, indices) {
		t.Fatal("fixture no longer isolates the loose destination endpoint cone")
	}
	for _, pair := range [][2]int{{1, 5}, {5, 1}} {
		if diagonalInt(pair[0], pair[1], n, verts, indices) || diagonalLoose(pair[0], pair[1], n, verts, indices) {
			t.Fatalf("accepted diagonal through exterior bridge wedge: %v", pair)
		}
	}
	for _, pair := range [][2]int{{1, 8}, {8, 1}} {
		if !diagonalInt(pair[0], pair[1], n, verts, indices) || !diagonalLoose(pair[0], pair[1], n, verts, indices) {
			t.Fatalf("rejected interior diagonal around the hole: %v", pair)
		}
	}
}

func TestDiagonalRetainsLooseBridgeRecovery(t *testing.T) {
	verts, indices := diagonalPolicyContour([][2]int{
		{0, 0}, {0, 10}, {10, 10}, {10, 0}, {0, 0},
		{4, 4}, {6, 4}, {6, 6}, {4, 6}, {4, 4},
	})
	for _, pair := range [][2]int{{0, 5}, {5, 0}} {
		if diagonalInt(pair[0], pair[1], len(indices), verts, indices) {
			t.Fatal("overlapping bridge was unexpectedly a strict diagonal")
		}
		if !diagonalLoose(pair[0], pair[1], len(indices), verts, indices) {
			t.Fatal("both-endpoint admission disabled legitimate loose bridge recovery")
		}
	}
}

func TestDiagonalPreservesSimpleConvexContours(t *testing.T) {
	verts, indices := diagonalPolicyContour([][2]int{{0, 0}, {0, 10}, {10, 10}, {10, 0}})
	for _, pair := range [][2]int{{0, 2}, {2, 0}, {1, 3}, {3, 1}} {
		if !diagonalInt(pair[0], pair[1], len(indices), verts, indices) || !diagonalLoose(pair[0], pair[1], len(indices), verts, indices) {
			t.Fatalf("rejected ordinary convex interior diagonal: %v", pair)
		}
	}
}

func diagonalPolicyContour(points [][2]int) (verts, indices []int) {
	for i, p := range points {
		verts = append(verts, p[0], 0, p[1], 0)
		indices = append(indices, i)
	}
	return
}
