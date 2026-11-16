package dynamic_test

import (
	"context"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func bakedBundle(t *testing.T, scene geom.Scene, c bake.Config) artifact.Bundle {
	t.Helper()
	prepared, err := bake.PrepareSource(scene, c.Settings.Origin, c.Strict)
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.BuildTiles(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result.Bundle
}

func TestAdoptPublishesVerifiedBundleAndAcceptsLiveUpdates(t *testing.T) {
	scene, c := testworld.Scene()
	w, err := dynamic.Adopt(context.Background(), scene, c, bakedBundle(t, scene, c))
	if err != nil {
		t.Fatal(err)
	}
	if projection(t, w.Snapshot(), 3.1).Code != navmesh.Complete {
		t.Fatal("adopted bundle lost the bridge")
	}
	if _, err = w.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	if projection(t, w.Snapshot(), 3.1).Code == navmesh.Complete || projection(t, w.Snapshot(), .1).Code != navmesh.Complete {
		t.Fatal("live update after adoption did not rebuild the affected columns")
	}
}

func TestAdoptRejectsBundlesFromOtherSourcesOrSettings(t *testing.T) {
	scene, c := testworld.Scene()
	bundle := bakedBundle(t, scene, c)

	changed := scene.Clone()
	changed.Instances = changed.Instances[:len(changed.Instances)-1]
	if _, err := dynamic.Adopt(context.Background(), changed, c, bundle); err == nil {
		t.Fatal("adopted a bundle baked from different geometry")
	}

	other := c
	other.Settings.MaxSimplificationError += .5
	if _, err := dynamic.Adopt(context.Background(), scene, other, bundle); err == nil {
		t.Fatal("adopted a bundle baked with different settings")
	}

	if len(bundle.Tiles) > 0 {
		partial := bundle
		partial.Tiles = bundle.Tiles[1:]
		if _, err := dynamic.Adopt(context.Background(), scene, c, partial); err == nil {
			t.Fatal("adopted a bundle missing a configured column")
		}
	}

	stale := bundle
	stale.Revision++
	if _, err := dynamic.Adopt(context.Background(), scene, c, stale); err == nil {
		t.Fatal("adopted a bundle from another revision")
	}
}
