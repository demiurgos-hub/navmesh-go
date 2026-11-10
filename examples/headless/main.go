// Command headless exercises pure-Go baking, queries, a topology update, and
// desired-state restoration. It requires no editor or prebuilt native tools.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx := context.Background()
	scene, config := testworld.Scene()
	world, err := dynamic.New(ctx, scene, config)
	if err != nil {
		return err
	}
	bundle, err := world.Snapshot().Bundle()
	if err != nil {
		return err
	}
	data, err := artifact.Encode(bundle)
	if err != nil {
		return err
	}
	decoded, err := artifact.Decode(data, artifact.DefaultLimits())
	if err != nil {
		return err
	}
	loaded, err := navmesh.NewSnapshot(decoded)
	if err != nil {
		return err
	}
	q, err := loaded.NewQuery(navmesh.Limits{})
	if err != nil {
		return err
	}
	opts := navmesh.ProjectionOptions{Horizontal: .5, Up: .4, Down: .4}
	a := q.Project(ctx, geom.Vec3{X: -5, Y: 3.1}, opts, navmesh.Filter{})
	b := q.Project(ctx, geom.Vec3{X: 5, Y: 3.1}, opts, navmesh.Filter{})
	if a.Code != navmesh.Complete || b.Code != navmesh.Complete {
		return fmt.Errorf("bridge projection failed")
	}
	path := q.FindPath(ctx, a.Surface, b.Surface, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		return fmt.Errorf("bridge route: %v", path.Status)
	}
	position := a.Surface
	for i := 0; i < 20; i++ {
		v := position.Position()
		v.X += .25
		move := q.Move(ctx, position, v, navmesh.Filter{})
		if move.Code != navmesh.Complete {
			return fmt.Errorf("movement: %v", move.Status)
		}
		position = move.Surface
	}
	if _, err = world.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}}); err != nil {
		return err
	}
	candidate, err := world.Prepare(ctx)
	if err != nil {
		return err
	}
	if err = world.Commit(candidate); err != nil {
		return err
	}
	current, _ := world.Snapshot().NewQuery(navmesh.Limits{})
	if current.Project(ctx, position.Position(), opts, navmesh.Filter{}).Code == navmesh.Complete {
		return fmt.Errorf("removed bridge still supports agent")
	}
	if current.Project(ctx, geom.Vec3{Y: .1}, opts, navmesh.Filter{}).Code != navmesh.Complete {
		return fmt.Errorf("underlying terrain missing")
	}
	checkpoint, err := world.Checkpoint("demo-tick-20")
	if err != nil {
		return err
	}
	restored, err := dynamic.Restore(ctx, checkpoint, "demo-tick-20")
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		ArtifactBytes, Tiles, RouteCorners  int
		CommittedRevision, RestoredRevision uint64
		RemovedBridge, RetainedGround       bool
	}{len(data), len(bundle.Tiles), len(path.Path.Corners()), world.Snapshot().Revision(), restored.Snapshot().Revision(), true, true})
}
