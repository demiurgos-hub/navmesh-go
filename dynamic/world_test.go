package dynamic_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func newWorld(t *testing.T) *dynamic.World {
	t.Helper()
	scene, c := testworld.Scene()
	w, e := dynamic.New(context.Background(), scene, c)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func commit(t *testing.T, w *dynamic.World) {
	t.Helper()
	c, e := w.Prepare(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Commit(c); e != nil {
		t.Fatal(e)
	}
}
func projection(t *testing.T, s *navmesh.Snapshot, y float64) navmesh.ProjectionResult {
	t.Helper()
	q, e := s.NewQuery(navmesh.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	return q.Project(context.Background(), geom.Vec3{X: 0, Y: y, Z: 0}, navmesh.ProjectionOptions{Horizontal: .3, Up: .3, Down: .3}, navmesh.Filter{})
}
func TestRemoveBridgeRetainsGroundAndOldSnapshot(t *testing.T) {
	w := newWorld(t)
	old := w.Acquire()
	defer old.Release()
	if projection(t, old.Snapshot, 3.1).Code != navmesh.Complete {
		t.Fatal("missing bridge")
	}
	if _, e := w.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}}); e != nil {
		t.Fatal(e)
	}
	commit(t, w)
	if projection(t, w.Snapshot(), 3.1).Code == navmesh.Complete {
		t.Fatal("bridge remains")
	}
	if projection(t, w.Snapshot(), .1).Code != navmesh.Complete {
		t.Fatal("ground disappeared")
	}
	if projection(t, old.Snapshot, 3.1).Code != navmesh.Complete {
		t.Fatal("old reader mutated")
	}
	data, e := w.Checkpoint("tick-12")
	if e != nil {
		t.Fatal(e)
	}
	restored, e := dynamic.Restore(context.Background(), data, "tick-12")
	if e != nil {
		t.Fatal(e)
	}
	if projection(t, restored.Snapshot(), 3.1).Code == navmesh.Complete || projection(t, restored.Snapshot(), .1).Code != navmesh.Complete {
		t.Fatal("bad restoration")
	}
}
func TestOverlapRemovalAndStaleCandidate(t *testing.T) {
	w := newWorld(t)
	a := geom.Volume{ID: "a", Shape: "box", Bounds: geom.Bounds{Min: geom.Vec3{X: -2, Y: 0, Z: -2}, Max: geom.Vec3{X: 2, Y: 2, Z: 2}}, Exclude: true}
	b := a
	b.ID = "b"
	if _, e := w.Apply(dynamic.Changes{Volumes: []geom.Volume{a, b}}); e != nil {
		t.Fatal(e)
	}
	c, e := w.Prepare(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Apply(dynamic.Changes{RemoveVolumes: []string{"a"}}); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(w.Commit(c), dynamic.ErrSuperseded) {
		t.Fatal("stale candidate committed")
	}
	commit(t, w)
	if projection(t, w.Snapshot(), .1).Code == navmesh.Complete {
		t.Fatal("overlap removed too early")
	}
	if _, e = w.Apply(dynamic.Changes{RemoveVolumes: []string{"b"}}); e != nil {
		t.Fatal(e)
	}
	commit(t, w)
	if projection(t, w.Snapshot(), .1).Code != navmesh.Complete {
		t.Fatal("underlying ground not restored")
	}
}
func TestQueriesDuringPublication(t *testing.T) {
	w := newWorld(t)
	lease := w.Acquire()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, e := lease.Snapshot.NewQuery(navmesh.Limits{})
			if e != nil {
				t.Error(e)
				return
			}
			for j := 0; j < 30; j++ {
				r := q.Project(context.Background(), geom.Vec3{Y: 3.1}, navmesh.ProjectionOptions{Horizontal: .3, Up: .3, Down: .3}, navmesh.Filter{})
				if r.Code != navmesh.Complete {
					t.Error(r.Status)
					return
				}
			}
		}()
	}
	if _, e := w.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}}); e != nil {
		t.Fatal(e)
	}
	commit(t, w)
	wg.Wait()
	lease.Release()
}

func TestAddSurfaceCancellationRevisionsAndRetention(t *testing.T) {
	w := newWorld(t)
	scene, _, _ := w.Desired()
	var bridge geom.Instance
	for _, v := range scene.Instances {
		if v.ID == "bridge-instance" {
			bridge = v
		}
	}
	if _, e := w.Apply(dynamic.Changes{RemoveInstances: []string{bridge.ID}}); e != nil {
		t.Fatal(e)
	}
	commit(t, w)
	lease0 := w.Acquire()
	defer lease0.Release()
	if _, e := w.Apply(dynamic.Changes{Instances: []geom.Instance{bridge}}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := w.Prepare(ctx); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel=%v", e)
	}
	if w.Status().State != dynamic.Cancelled || len(w.Status().Dirty) == 0 || projection(t, w.Snapshot(), 3.1).Code == navmesh.Complete {
		t.Fatal("cancel published or dropped desired change")
	}
	commit(t, w)
	if projection(t, w.Snapshot(), 3.1).Code != navmesh.Complete {
		t.Fatal("added surface absent")
	}
	lease1 := w.Acquire()
	defer lease1.Release()
	bridge.Transform = geom.Translation(geom.Vec3{Y: 1})
	if _, e := w.Apply(dynamic.Changes{Instances: []geom.Instance{bridge}}); e == nil {
		t.Fatal("accepted changed object at old explicit revision")
	}
	bridge.Revision = 0
	if _, e := w.Apply(dynamic.Changes{Instances: []geom.Instance{bridge}}); e != nil {
		t.Fatal(e)
	}
	commit(t, w)
	if projection(t, w.Snapshot(), 4.1).Code != navmesh.Complete || projection(t, w.Snapshot(), 3.1).Code == navmesh.Complete {
		t.Fatal("moved surface did not dirty old and new bounds")
	}
	if _, e := w.Apply(dynamic.Changes{RemoveInstances: []string{bridge.ID}}); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Prepare(context.Background()); !errors.Is(e, dynamic.ErrBusy) {
		t.Fatalf("retention limit ignored: %v", e)
	}
	lease0.Release()
	commit(t, w)
}

func BenchmarkAffectedRebuild(b *testing.B) {
	scene, cfg := testworld.Scene()
	w, e := dynamic.New(context.Background(), scene, cfg)
	if e != nil {
		b.Fatal(e)
	}
	v := geom.Volume{ID: "blocker", Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: -1, Z: -1}, Max: geom.Vec3{X: 1, Y: 2, Z: 1}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Disabled = i%2 == 1
		if _, e = w.Apply(dynamic.Changes{Volumes: []geom.Volume{v}}); e != nil {
			b.Fatal(e)
		}
		c, e := w.Prepare(context.Background())
		if e != nil {
			b.Fatal(e)
		}
		if e = w.Commit(c); e != nil {
			b.Fatal(e)
		}
	}
}

func TestOverheadExclusionDirtiesStandingSpace(t *testing.T) {
	scene, cfg := testworld.Scene()
	scene.Instances = scene.Instances[:1]
	cfg.Settings.Bounds.Max.Y = 1
	w, e := dynamic.New(context.Background(), scene, cfg)
	if e != nil {
		t.Fatal(e)
	}
	v := geom.Volume{ID: "overhead", Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: -1, Y: 1.1, Z: -1}, Max: geom.Vec3{X: 1, Y: 1.5, Z: 1}}}
	u, e := w.Apply(dynamic.Changes{Volumes: []geom.Volume{v}})
	if e != nil {
		t.Fatal(e)
	}
	if len(u.Dirty) == 0 {
		t.Fatal("clearance overlap did not dirty ground tiles")
	}
	commit(t, w)
	if projection(t, w.Snapshot(), .1).Code == navmesh.Complete {
		t.Fatal("overhead blocker did not remove standing space")
	}
	if _, e = w.Apply(dynamic.Changes{RemoveVolumes: []string{v.ID}}); e != nil {
		t.Fatal(e)
	}
	commit(t, w)
	if projection(t, w.Snapshot(), .1).Code != navmesh.Complete {
		t.Fatal("overhead removal did not restore ground")
	}
}

func TestConcurrentDesiredChangesAndCheapPending(t *testing.T) {
	w := newWorld(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := geom.Volume{ID: string(rune('a' + i)), Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: float64(i) - 2, Z: -1}, Max: geom.Vec3{X: float64(i) - 1, Y: 2, Z: 1}}}
			for {
				_, e := w.Apply(dynamic.Changes{Volumes: []geom.Volume{v}})
				if errors.Is(e, dynamic.ErrSuperseded) {
					continue
				}
				if e != nil {
					t.Error(e)
				}
				return
			}
		}(i)
	}
	wg.Wait()
	scene, _, _ := w.Desired()
	if len(scene.Volumes) != 4 || !w.Pending() {
		t.Fatal("concurrent change lost or readiness false")
	}
	if n := testing.AllocsPerRun(100, func() { _ = w.Pending() }); n != 0 {
		t.Fatalf("hot readiness check allocates %g", n)
	}
	commit(t, w)
	if w.Pending() {
		t.Fatal("committed update still pending")
	}
}
