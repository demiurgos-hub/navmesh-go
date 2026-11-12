package navmesh

import (
	"context"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestFollowerScratchTracksLocalWork(t *testing.T) {
	ctx := context.Background()
	scene, cfg := testworld.Scene()
	built, err := bake.Build(ctx, scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshot(built.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	search, _ := snapshot.NewQuery(Limits{})
	project := func(x float64) Surface {
		r := search.Project(ctx, Vec3{X: x, Y: 3.1}, ProjectionOptions{Horizontal: 1, Up: .2, Down: .2}, Filter{})
		if r.Code != Complete {
			t.Fatal(r.Status)
		}
		return r.Surface
	}
	a, z := project(-5), project(5)
	path := search.FindPath(ctx, a, z, Filter{})
	if path.Code != Complete {
		t.Fatal(path.Status)
	}
	ownedCorners, ownedTiles := path.Path.Corners(), path.Path.Tiles()
	follower, _ := snapshot.NewQuery(Limits{})
	result := follower.RepairPath(ctx, path.Path, a, Filter{})
	if result.Code != Complete || !reflect.DeepEqual(result.Path.Corners(), ownedCorners) {
		t.Fatalf("repair changed unchanged corridor: %+v", result.Status)
	}
	if cap(follower.refs) > len(path.Path.refs) || cap(follower.points) > (2*len(path.Path.refs)+2)*3 {
		t.Fatal("short repair allocated maximum query capacity")
	}
	mover, _ := snapshot.NewQuery(Limits{})
	if r := mover.MoveWithoutVisited(ctx, a, a.Position(), Filter{}); r.Code != Complete {
		t.Fatal(r.Status)
	}
	if cap(mover.refs) > detour.DT_MAX_LOCAL_NODES+1 {
		t.Fatal("local movement allocated a global corridor buffer")
	}
	// Small and long operations may resize the search's scratch, but retained
	// route values belong to their callers and remain unchanged.
	search.RepairPath(ctx, result.Path, result.Path.End(), Filter{})
	if !reflect.DeepEqual(path.Path.Corners(), ownedCorners) || !reflect.DeepEqual(path.Path.Tiles(), ownedTiles) {
		t.Fatal("smaller scratch operation changed an owned result")
	}
}
