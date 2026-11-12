package dynamic_test

import (
	"context"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func scopedPath(t *testing.T, snapshot *navmesh.Snapshot) (navmesh.Surface, *navmesh.Path) {
	t.Helper()
	q := regionalQuery(t, snapshot)
	start := regionalSupport(t, q, geom.Vec3{X: -24, Y: .1})
	end := regionalSupport(t, q, geom.Vec3{X: -20, Y: .1, Z: 2})
	path := q.FindPath(context.Background(), start, end, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	return start, path.Path
}

func TestScopedSafetyRejectsIdenticalForeignSnapshots(t *testing.T) {
	w := regionalWorld(t)
	old := w.Acquire()
	defer old.Release()
	location, path := scopedPath(t, old.Snapshot)
	bundle, err := old.Snapshot.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := navmesh.NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	foreignLocation, foreignPath := scopedPath(t, foreign)
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("right", 20)}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	global := w.Safety()
	scope := global.ForRevision(old.Snapshot.Revision())
	if !global.SafeLocation(foreignLocation) || !global.SafePath(foreignPath) {
		t.Fatal("global dependency-based compatibility changed")
	}
	if !scope.SafeLocation(location) || !scope.SafePath(path) || scope.SafeLocation(foreignLocation) || scope.SafePath(foreignPath) {
		t.Fatal("scope failed to distinguish actual leased snapshot from identical artifact")
	}
	if scope.SafeLocation(navmesh.Surface{}) || scope.SafePath(nil) || global.ForRevision(foreign.Revision()+100).SafePath(path) {
		t.Fatal("zero or unknown handle accepted")
	}
	t.Run("OldScopeAllocations", func(t *testing.T) {
		if count := testing.AllocsPerRun(100, func() {
			if s := w.Safety().ForRevision(old.Snapshot.Revision()); s != scope || !s.SafeLocation(location) || !s.SafePath(path) {
				panic("retained scope changed")
			}
		}); count != 0 {
			t.Fatalf("old scoped reads allocate: %g", count)
		}
	})
}

func TestScopedSafetyDuplicateRevisionTracksExactLiveIdentities(t *testing.T) {
	w := regionalWorld(t)
	old, second := w.Acquire(), w.Acquire()
	defer old.Release()
	defer second.Release()
	location, path := scopedPath(t, old.Snapshot)
	// Explicit preparation without a desired edit publishes a distinct runtime
	// snapshot with the same revision and no changed-column history.
	commit(t, w)
	current := w.Acquire()
	defer current.Release()
	newLocation, newPath := scopedPath(t, current.Snapshot)
	if current.Snapshot == old.Snapshot || current.Snapshot.Revision() != old.Snapshot.Revision() {
		t.Fatal("fixture does not contain distinct identities at one revision")
	}
	before := w.Safety()
	scope := before.ForRevision(current.Snapshot.Revision())
	if before.HasQuarantine() || !scope.SafeLocation(location) || !scope.SafePath(path) || !scope.SafeLocation(newLocation) || !scope.SafePath(newPath) {
		t.Fatal("same-revision scope lost an actual current/leased identity")
	}
	old.Release()
	if w.Safety() != before {
		t.Fatal("partial lease release changed identity membership")
	}
	second.Release()
	after := w.Safety()
	next := after.ForRevision(current.Snapshot.Revision())
	if after.Token() == before.Token() || next.Token() == scope.Token() || next.SafeLocation(location) || next.SafePath(path) || !next.SafeLocation(newLocation) || !next.SafePath(newPath) {
		t.Fatal("last old lease failed to remove exact identity from fresh capture")
	}
	if !scope.SafeLocation(location) || !scope.SafePath(path) {
		t.Fatal("release mutated an earlier immutable capture")
	}
	current.Release()
	if w.Safety() != after {
		t.Fatal("current snapshot identity disappeared with its last lease")
	}
}

func TestScopedSafetyRetiresZeroDirtyRevision(t *testing.T) {
	w := regionalWorld(t)
	old := w.Acquire()
	defer old.Release()
	location, path := scopedPath(t, old.Snapshot)
	unused := testworld.Plane("unused", geom.Vec3{X: -1, Z: -1}, geom.Vec3{X: 1, Z: 1})
	update, err := w.Apply(dynamic.Changes{Assets: []geom.Asset{unused}})
	if err != nil || len(update.Dirty) != 0 {
		t.Fatal("unused asset should advance desired identity without dirty columns", update, err)
	}
	commit(t, w)
	before := w.Safety()
	scope := before.ForRevision(old.Snapshot.Revision())
	if before.HasQuarantine() || scope == nil || !scope.SafeLocation(location) || !scope.SafePath(path) {
		t.Fatal("zero-dirty retained generation lost safety")
	}
	old.Release()
	if after := w.Safety(); after.Token() == before.Token() || after.ForRevision(old.Snapshot.Revision()) != nil {
		t.Fatal("fresh capture retained an unleased generation without quarantine")
	}
}

func TestScopedSafetyBlocksChangedOffMeshAndEveryLayer(t *testing.T) {
	scene, cfg := testworld.Scene()
	link := geom.Link{ID: "stairs", Start: geom.Vec3{X: -4, Y: .1}, End: geom.Vec3{X: -4, Y: 3.1}, Radius: .4, Flags: 1, Action: "climb"}
	scene.Links = []geom.Link{link}
	w, err := dynamic.New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	old := w.Acquire()
	defer old.Release()
	q := regionalQuery(t, old.Snapshot)
	ground := regionalSupport(t, q, link.Start)
	upper := regionalSupport(t, q, link.End)
	path := q.FindPath(context.Background(), ground, upper, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	hasLink := false
	for _, corner := range path.Path.Corners() {
		hasLink = hasLink || corner.Link != nil
	}
	if !hasLink {
		t.Fatal("fixture path does not traverse authored link")
	}
	link.Action = "changed-climb"
	if _, err := w.Apply(dynamic.Changes{Links: []geom.Link{link}}); err != nil {
		t.Fatal(err)
	}
	for phase := 0; phase < 2; phase++ {
		scope := w.Safety().ForRevision(old.Snapshot.Revision())
		if scope.SafeLocation(ground) || scope.SafeLocation(upper) || scope.SafePath(path.Path) || scope.SafeSegment(link.Start, link.End) {
			t.Fatal("changed link metadata escaped all-floor quarantine", phase)
		}
		if phase == 0 {
			commit(t, w)
		}
	}
	current := w.Acquire()
	defer current.Release()
	currentQuery := regionalQuery(t, current.Snapshot)
	newGround := regionalSupport(t, currentQuery, link.Start)
	newUpper := regionalSupport(t, currentQuery, link.End)
	latest := w.Safety().ForRevision(current.Snapshot.Revision())
	if !latest.SafeLocation(newGround) || !latest.SafeLocation(newUpper) {
		t.Fatal("current view inherited old link quarantine")
	}
	dependency, _ := newUpper.Tile()
	column := artifact.TileKey{X: dependency.Key.X, Z: dependency.Key.Z}
	if _, err := w.SetLoaded([]artifact.TileKey{column}, false); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	scope := w.Safety().ForRevision(current.Snapshot.Revision())
	if scope.SafeLocation(newGround) || scope.SafeLocation(newUpper) || scope.SafeBounds(link.Start, link.End) {
		t.Fatal("unloaded column escaped scoped quarantine")
	}
}
