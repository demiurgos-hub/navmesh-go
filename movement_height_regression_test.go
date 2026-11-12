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

func TestMovementHeightAcrossThinDetailTriangles(t *testing.T) {
	data, err := os.ReadFile("testdata/movement-detail-height.json")
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
	ctx := context.Background()
	start := navmesh.Vec3{X: 95.76627349853516, Y: .4000493884086609, Z: 92.9614486694336}
	projected := query.Project(ctx, start, navmesh.ProjectionOptions{Horizontal: .2, Up: .2, Down: .2}, navmesh.Filter{})
	if projected.Code != navmesh.Complete {
		t.Fatal(projected.Status)
	}
	end := navmesh.Vec3{X: 96, Y: .40000057220458984, Z: 96}
	distance := math.Sqrt((start.X-end.X)*(start.X-end.X) + (start.Y-end.Y)*(start.Y-end.Y) + (start.Z-end.Z)*(start.Z-end.Z))
	// These exact captured-direction steps failed while neighboring steps
	// succeeded. Expected heights come from the pinned standalone C++ runner.
	for _, tc := range []struct{ step, height float64 }{{.15, .40000200271606445}, {.2, .4000012278556824}, {.3, .4000020921230316}} {
		target := navmesh.Vec3{X: start.X + (end.X-start.X)*tc.step/distance, Y: start.Y + (end.Y-start.Y)*tc.step/distance, Z: start.Z + (end.Z-start.Z)*tc.step/distance}
		result := query.MoveWithoutVisited(ctx, projected.Surface, target, navmesh.Filter{})
		if result.Code != navmesh.Complete || math.Abs(result.Surface.Position().Y-tc.height) > 1e-6 {
			t.Fatalf("step=%g: status=%+v position=%+v", tc.step, result.Status, result.Surface.Position())
		}
		if !result.Normal.Finite() || result.Normal.Y <= .5 {
			t.Fatalf("step=%g invalid walkable ground normal: %+v", tc.step, result.Normal)
		}
	}
	// Cover rounded boundaries on both sides of the thin detail triangles;
	// every returned selected support must itself remain height-sampleable.
	for degree := 0; degree < 360; degree++ {
		angle := float64(degree) * math.Pi / 180
		target := navmesh.Vec3{X: start.X + .2*math.Cos(angle), Y: start.Y, Z: start.Z + .2*math.Sin(angle)}
		result := query.MoveWithoutVisited(ctx, projected.Surface, target, navmesh.Filter{})
		if result.Code != navmesh.Complete {
			t.Fatalf("direction=%d: %+v", degree, result.Status)
		}
		normal := result.Normal
		length := math.Sqrt(normal.X*normal.X + normal.Y*normal.Y + normal.Z*normal.Z)
		if !normal.Finite() || normal.Y <= .5 || math.Abs(length-1) > 1e-5 {
			t.Fatalf("direction=%d invalid walkable ground normal: %+v", degree, normal)
		}
		height := query.Height(ctx, result.Surface)
		if height.Code != navmesh.Complete || math.Abs(height.Height-result.Surface.Position().Y) > 1e-6 {
			t.Fatalf("direction=%d height disagrees with selected support: %+v", degree, height)
		}
	}
}
