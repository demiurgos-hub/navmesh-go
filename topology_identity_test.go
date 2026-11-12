package navmesh_test

import (
	"context"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
)

func TestTopologyIdentityKeepsForeignRevisionValidation(t *testing.T) {
	snapshot, query := setup(t)
	start, end := project(t, query, -5, 3.1, 0), project(t, query, 5, 3.1, 0)
	path := query.FindPath(context.Background(), start, end, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	topology := snapshot.Topology()
	if !topology.OwnsSurface(start) || !topology.OwnsPath(path.Path) {
		t.Fatal("exact snapshot ownership lost")
	}
	want, _ := start.Tile()
	if got, ok := topology.UnchangedSurfaceTile(start); !ok || got != want || !topology.PathUnchanged(path.Path) {
		t.Fatal("own immutable dependency rejected")
	}
	bundle, err := snapshot.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	identical, err := navmesh.NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !identical.Topology().PathUnchanged(path.Path) || !identical.Topology().SurfaceUnchanged(start) {
		t.Fatal("equivalent foreign metadata rejected")
	}
	if identical.Topology().OwnsSurface(start) || identical.Topology().OwnsPath(path.Path) {
		t.Fatal("equivalent foreign metadata mistaken for exact ownership")
	}
	for i := range bundle.Tiles {
		bundle.Tiles[i].Revision++
	}
	changed, err := navmesh.NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision() != snapshot.Revision() {
		t.Fatal("fixture does not collide on snapshot revision")
	}
	if changed.Topology().PathUnchanged(path.Path) || changed.Topology().SurfaceUnchanged(start) {
		t.Fatal("foreign snapshot skipped tile validation on colliding world/revision")
	}
	if _, ok := changed.Topology().UnchangedSurfaceTile(start); ok {
		t.Fatal("changed foreign tile reported unchanged")
	}
	var zero navmesh.Topology
	if zero.OwnsSurface(start) || zero.OwnsPath(path.Path) || topology.OwnsSurface(navmesh.Surface{}) || topology.OwnsPath(nil) {
		t.Fatal("zero ownership accepted")
	}
	if zero.PathUnchanged(path.Path) || zero.SurfaceUnchanged(start) || topology.PathUnchanged(nil) || topology.SurfaceUnchanged(navmesh.Surface{}) {
		t.Fatal("zero identity/handle accepted")
	}
	t.Run("TopologyAllocations", func(t *testing.T) {
		if count := testing.AllocsPerRun(100, func() {
			if _, ok := topology.UnchangedSurfaceTile(start); !ok || !topology.PathUnchanged(path.Path) || !topology.OwnsPath(path.Path) || !topology.OwnsSurface(start) {
				panic("own dependencies changed")
			}
		}); count != 0 {
			t.Fatalf("topology hot reads allocate: %g", count)
		}
	})
}
