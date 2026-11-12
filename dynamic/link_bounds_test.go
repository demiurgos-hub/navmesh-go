package dynamic_test

import (
	"context"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestAttachedLinkAboveRegionDirtiesActionChange(t *testing.T) {
	scene := geom.Scene{
		Assets: []geom.Asset{
			testworld.Plane("left", geom.Vec3{X: -6, Y: 3, Z: -3}, geom.Vec3{X: -1, Y: 3, Z: 3}),
			testworld.Plane("right", geom.Vec3{X: 1, Y: 3, Z: -3}, geom.Vec3{X: 6, Y: 3, Z: 3}),
		},
		Instances: []geom.Instance{{ID: "left", AssetID: "left", Transform: geom.Identity()}, {ID: "right", AssetID: "right", Transform: geom.Identity()}},
	}
	link := geom.Link{ID: "cross", Start: geom.Vec3{X: -2, Y: 3.4, Z: 1}, End: geom.Vec3{X: 2, Y: 3.4, Z: 1}, Radius: .05, Flags: 1, Action: "old-action"}
	scene.Links = []geom.Link{link}
	cfg := bake.DefaultConfig("link-height", geom.Bounds{Min: geom.Vec3{X: -8, Y: -1, Z: -4}, Max: geom.Vec3{X: 8, Y: 3.2, Z: 4}})
	cfg.Settings.TileSize = 32
	w, err := dynamic.New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	old := w.Acquire()
	defer old.Release()
	states := old.Snapshot.LinkStates()
	if len(states) != 1 || !states[0].Available || link.Start.Y-link.Radius <= cfg.Settings.Bounds.Max.Y {
		t.Fatalf("fixture must attach a link wholly above source selection Y: %+v", states)
	}
	q := regionalQuery(t, old.Snapshot)
	start := regionalSupport(t, q, states[0].Start)
	end := regionalSupport(t, q, states[0].End)
	path := q.FindPath(context.Background(), start, end, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	link.Action = "new-action"
	update, err := w.Apply(dynamic.Changes{Links: []geom.Link{link}})
	if err != nil {
		t.Fatal(err)
	}
	pending := w.Safety().ForRevision(old.Snapshot.Revision())
	if len(update.Dirty) == 0 || pending.SafePath(path.Path) || pending.SafeLocation(start) || pending.SafeSegment(start.Position(), end.Position()) {
		t.Fatalf("attached link action change escaped pre-publication quarantine: dirty=%v", update.Dirty)
	}
	commit(t, w)
	current := w.Acquire()
	defer current.Release()
	latest := regionalQuery(t, current.Snapshot)
	transfer := latest.RebaseUnchangedPath(context.Background(), path.Path, start, start.Position(), .05, navmesh.Filter{})
	if transfer.Code != navmesh.Stale || transfer.Path != nil {
		t.Fatalf("strict transfer retained old traversal action: %+v", transfer.Status)
	}
	if w.Safety().ForRevision(old.Snapshot.Revision()).SafePath(path.Path) {
		t.Fatal("publication lost old-view link quarantine")
	}
	newStart := regionalSupport(t, latest, states[0].Start)
	newEnd := regionalSupport(t, latest, states[0].End)
	newPath := latest.FindPath(context.Background(), newStart, newEnd, navmesh.Filter{})
	if newPath.Code != navmesh.Complete {
		t.Fatal(newPath.Status)
	}
	found := false
	for _, corner := range newPath.Path.Corners() {
		if corner.Link != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("rebuilt route has no traversal")
	}
	currentBundle, err := current.Snapshot.Bundle()
	if err != nil || len(currentBundle.Links) != 1 || currentBundle.Links[0].Action != link.Action {
		t.Fatal("rebuilt snapshot retained prior action metadata", err)
	}
}
