package navmesh_test

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
)

func TestProjectionDoesNotConfuseSlopeNeighborsWithStackedFloors(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		point   navmesh.Vec3
	}{
		{"projection-human-slope.json", navmesh.Vec3{X: 28.132944107, Y: -.381159037, Z: -93.158912659}},
		{"projection-large-seam.json", navmesh.Vec3{X: -95.809951782, Y: -.501140177, Z: -63.994953156}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			// These are reduced baked tiles from captured terrain rehearsal
			// failures. Exact support was incorrectly tied with a neighbor
			// 6.7mm (human) or 5.0mm (large) away on the same sloping floor.
			data, err := os.ReadFile("testdata/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var bundle artifact.Bundle
			if err := json.Unmarshal(data, &bundle); err != nil {
				t.Fatal(err)
			}
			snapshot, err := navmesh.NewSnapshot(bundle)
			if err != nil {
				t.Fatal(err)
			}
			query, err := snapshot.NewQuery(navmesh.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			result := query.Project(context.Background(), tc.point, navmesh.ProjectionOptions{Horizontal: 1, Up: .8, Down: .8}, navmesh.Filter{})
			if result.Code != navmesh.Complete {
				t.Fatal(result.Status)
			}
			point := result.Surface.Position()
			if math.Abs(point.X-tc.point.X) > 1e-5 || math.Abs(point.Y-tc.point.Y) > 1e-5 || math.Abs(point.Z-tc.point.Z) > 1e-5 {
				t.Fatalf("did not retain the nearest support: got %+v want %+v", point, tc.point)
			}
		})
	}
}
