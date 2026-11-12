package dynamic_test

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestApplyOwnsUpsertsAndPreservesAcceptedSourceOnFailure(t *testing.T) {
	w := newWorld(t)
	asset := testworld.Plane("unused", geom.Vec3{}, geom.Vec3{X: 1, Z: 1})
	volume := geom.Volume{ID: "paint", Shape: "convex", Bounds: geom.Bounds{Min: geom.Vec3{}, Max: geom.Vec3{X: 1, Y: 2, Z: 1}}, Points: []geom.Vec3{{}, {Z: 1}, {X: 1}}, Area: 1}
	link := geom.Link{ID: "stairs", Start: geom.Vec3{X: -4, Y: .1}, End: geom.Vec3{X: -4, Y: 3.1}, Radius: .4, Flags: 1, Profiles: []string{"human"}}
	if _, err := w.Apply(dynamic.Changes{Assets: []geom.Asset{asset}, Volumes: []geom.Volume{volume}, Links: []geom.Link{link}}); err != nil {
		t.Fatal(err)
	}
	accepted, _, revision := w.Desired()
	fingerprint, err := accepted.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	asset.Vertices[0].X = math.NaN()
	volume.Points[0].X = math.NaN()
	link.Profiles[0] = "different"
	current, _, _ := w.Desired()
	got, err := current.Fingerprint()
	if err != nil || got != fingerprint {
		t.Fatal("retained upserts alias caller", err)
	}
	accepted.Assets[0].Vertices[0].X = math.NaN()
	if _, err = w.Apply(dynamic.Changes{Assets: []geom.Asset{asset}}); err == nil {
		t.Fatal("invalid source edit accepted")
	}
	current, _, after := w.Desired()
	got, err = current.Fingerprint()
	if err != nil || after != revision || got != fingerprint {
		t.Fatal("failed edit replaced accepted sources", err)
	}
	commit(t, w)
}

func TestSourceMetadataPreservesDirtyColumnsAtRegionOrigin(t *testing.T) {
	scene, cfg := testworld.Scene()
	a, err := dynamic.New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := range scene.Instances {
		scene.Instances[i].Transform[3] = 1000
	}
	cfg.Settings.Origin.X = 1000
	cfg.Settings.Bounds.Min.X += 1000
	cfg.Settings.Bounds.Max.X += 1000
	b, err := dynamic.New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	x, err := a.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}})
	if err != nil {
		t.Fatal(err)
	}
	y, err := b.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x.Dirty, y.Dirty) {
		t.Fatalf("region origin changed dirty columns: %v versus %v", x.Dirty, y.Dirty)
	}
	commit(t, b)
}

func TestRevisionOnlyUpdateRetainsGeometryMetadata(t *testing.T) {
	w := newWorld(t)
	scene, _, _ := w.Desired()
	instance := scene.Instances[0]
	instance.Revision++
	u, err := w.Apply(dynamic.Changes{Instances: []geom.Instance{instance}})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Dirty) != 0 {
		t.Fatalf("revision-only update rebuilt geometry: %v", u.Dirty)
	}
	commit(t, w)
	if w.Pending() {
		t.Fatal("metadata-only publication stayed pending")
	}
	if _, err = w.Apply(dynamic.Changes{}); err != nil {
		t.Fatal(err)
	}
	if w.Pending() {
		t.Fatal("empty edit changed canonical source fingerprint")
	}
}
