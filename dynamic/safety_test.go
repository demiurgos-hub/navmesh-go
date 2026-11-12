package dynamic_test

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func regionalWorld(t *testing.T) *dynamic.World {
	t.Helper()
	asset := testworld.Plane("floor", geom.Vec3{X: -32, Z: -8}, geom.Vec3{X: 32, Z: 8})
	scene := geom.Scene{Assets: []geom.Asset{asset}, Instances: []geom.Instance{{ID: "floor", AssetID: "floor", Transform: geom.Identity()}}}
	cfg := bake.DefaultConfig("regional", geom.Bounds{Min: geom.Vec3{X: -32, Y: -1, Z: -8}, Max: geom.Vec3{X: 32, Y: 6, Z: 8}})
	cfg.Settings.TileSize = 16
	w, err := dynamic.New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func regionalQuery(t *testing.T, snapshot *navmesh.Snapshot) *navmesh.Query {
	t.Helper()
	q, err := snapshot.NewQuery(navmesh.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func regionalSupport(t *testing.T, q *navmesh.Query, p geom.Vec3) navmesh.Surface {
	t.Helper()
	r := q.Project(context.Background(), p, navmesh.ProjectionOptions{Horizontal: .3, Up: .3, Down: .3}, navmesh.Filter{})
	if r.Code != navmesh.Complete {
		t.Fatalf("support %v: %+v", p, r.Status)
	}
	return r.Surface
}

func regionalBlocker(id string, x float64) geom.Volume {
	return geom.Volume{ID: id, Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: x - 1, Z: -1}, Max: geom.Vec3{X: x + 1, Y: 2, Z: 1}}}
}

func TestRegionalSafetyPendingFailureAndCoalescing(t *testing.T) {
	w := regionalWorld(t)
	lease := w.Acquire()
	defer lease.Release()
	q := regionalQuery(t, lease.Snapshot)
	left := regionalSupport(t, q, geom.Vec3{X: -24, Y: .1})
	leftEnd := regionalSupport(t, q, geom.Vec3{X: -20, Y: .1, Z: 2})
	right := regionalSupport(t, q, geom.Vec3{X: 20, Y: .1})
	path := q.FindPath(context.Background(), left, leftEnd, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	before := w.Safety()
	if !before.SafeLocation(right) {
		t.Fatal("initial support unsafe")
	}
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("right", 20)}}); err != nil {
		t.Fatal(err)
	}
	pending := w.Safety()
	if pending == before || pending.Token() == before.Token() || pending.CommittedRevision() != before.CommittedRevision() || pending.DesiredRevision() <= pending.CommittedRevision() {
		t.Fatal("incoherent pending capture")
	}
	if !before.SafeLocation(right) || pending.SafeLocation(right) || !pending.SafeLocation(left) || !pending.SafePath(path.Path) {
		t.Fatal("quarantine changed old capture or stopped distant support/path")
	}
	from, to := geom.Vec3{X: -28, Y: 100}, geom.Vec3{X: 28, Y: 100}
	if !pending.SafeSegment(from, from) || !pending.SafeSegment(to, to) || pending.SafeSegment(from, to) {
		t.Fatal("whole segment/stacked-height quarantine failed")
	}
	if pending.SafeBounds(from, to) || pending.SafeSegment(geom.Vec3{X: math.NaN()}, to) || pending.SafeBounds(to, from) {
		t.Fatal("bounds/invalid geometry accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Prepare(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if w.Safety() != pending || w.Safety().SafeLocation(right) {
		t.Fatal("failed build dropped quarantine")
	}
	candidate, err := w.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("left", -24)}}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(w.Commit(candidate), dynamic.ErrSuperseded) {
		t.Fatal("stale commit accepted")
	}
	if w.Safety().SafeLocation(left) || w.Safety().SafeLocation(right) || !pending.SafeLocation(left) {
		t.Fatal("coalescing lost a region or mutated capture")
	}
	commit(t, w)
	if w.Safety().DesiredRevision() != w.Safety().CommittedRevision() || w.Safety().SafeLocation(left) || w.Safety().SafeLocation(right) {
		t.Fatal("published changed support was accepted as unchanged")
	}
}

func TestRegionalSafetyRebaseBoundsManagedRetention(t *testing.T) {
	w := regionalWorld(t)
	lease := w.Acquire()
	defer func() { lease.Release() }()
	q := regionalQuery(t, lease.Snapshot)
	location := regionalSupport(t, q, geom.Vec3{X: -24, Y: .1})
	end := regionalSupport(t, q, geom.Vec3{X: -20, Y: .1, Z: 2})
	path := q.FindPath(context.Background(), location, end, navmesh.Filter{}).Path
	if path == nil {
		t.Fatal("missing path")
	}
	blocker := regionalBlocker("right", 20)
	for i := 0; i < 5; i++ {
		blocker.Disabled = i%2 != 0
		if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{blocker}}); err != nil {
			t.Fatal(err)
		}
		commit(t, w)
		safety := w.Safety()
		if !safety.SafePath(path) || !safety.SafeLocation(location) {
			t.Fatal("unrelated update invalidated route")
		}
		next := w.Acquire()
		nextQ := regionalQuery(t, next.Snapshot)
		r := nextQ.RebasePath(context.Background(), path, location, location.Position(), .05, navmesh.Filter{})
		if r.Code != navmesh.Complete || r.Path == nil || r.Surface.Revision() != next.Snapshot.Revision() {
			next.Release()
			t.Fatalf("rebase %+v", r.Status)
		}
		if stats := w.RetentionStats(); stats.RetiredGenerations != 1 || stats.Leases != 2 {
			t.Fatalf("retention before release %+v", stats)
		}
		lease.Release()
		lease, q, location, path = next, nextQ, r.Surface, r.Path
		if stats := w.RetentionStats(); stats.RetiredGenerations != 0 || stats.Leases != 1 {
			t.Fatalf("retention after release %+v", stats)
		}
	}
	if r := q.RebasePath(context.Background(), nil, location, location.Position(), .05, navmesh.Filter{}); r.Code != navmesh.Complete || r.Path != nil || !r.Surface.Valid() {
		t.Fatal("support-only rebase failed", r.Status)
	}
	p := location.Position()
	p.Y += 1
	if r := q.RebasePath(context.Background(), nil, location, p, .05, navmesh.Filter{}); r.Code != navmesh.NoSurface {
		t.Fatal("unsupported body admitted", r.Status)
	}
	t.Run("SafetyAllocations", func(t *testing.T) {
		if n := testing.AllocsPerRun(100, func() {
			s := w.Safety()
			if !s.SafePath(path) || !s.SafeLocation(location) || !s.SafeSegment(location.Position(), location.Position()) {
				panic("unsafe")
			}
		}); n != 0 {
			t.Fatalf("hot safety checks allocate: %v", n)
		}
	})
}

func TestRegionalRebaseDoesNotSelectGroundAfterBridgeRemoval(t *testing.T) {
	w := newWorld(t)
	old := w.Acquire()
	defer old.Release()
	q := regionalQuery(t, old.Snapshot)
	upper := regionalSupport(t, q, geom.Vec3{Y: 3.1})
	if _, err := w.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}}); err != nil {
		t.Fatal(err)
	}
	if w.Safety().SafeLocation(upper) || w.Safety().SafeSegment(geom.Vec3{Y: 100}, geom.Vec3{Y: 100}) {
		t.Fatal("stacked support escaped quarantine")
	}
	commit(t, w)
	current := regionalQuery(t, w.Snapshot())
	if r := current.RebasePath(context.Background(), nil, upper, upper.Position(), 10, navmesh.Filter{}); r.Code != navmesh.Stale || r.Surface.Valid() {
		t.Fatal("removed bridge was reprojected", r.Status)
	}
}

func TestRegionalUnloadedColumnsRemainQuarantined(t *testing.T) {
	w := regionalWorld(t)
	old := w.Acquire()
	defer old.Release()
	q := regionalQuery(t, old.Snapshot)
	right := regionalSupport(t, q, geom.Vec3{X: 20, Y: .1})
	tile, ok := right.Tile()
	if !ok {
		t.Fatal("missing tile")
	}
	key := artifact.TileKey{X: tile.Key.X, Z: tile.Key.Z}
	if _, err := w.SetLoaded([]artifact.TileKey{key}, false); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	if w.Pending() || w.Safety().SafeLocation(right) || w.Safety().SafeSegment(right.Position(), right.Position()) {
		t.Fatal("unloaded column became safe")
	}
	if _, err := w.SetLoaded([]artifact.TileKey{key}, true); err != nil {
		t.Fatal(err)
	}
	if w.Safety().SafeSegment(right.Position(), right.Position()) {
		t.Fatal("reload intent cleared quarantine early")
	}
	commit(t, w)
	if w.Safety().SafeSegment(right.Position(), right.Position()) {
		t.Fatal("reload exposed a changed column to an old retained view")
	}
	old.Release()
	if !w.Safety().SafeSegment(right.Position(), right.Position()) {
		t.Fatal("successful reload stayed quarantined after retirement")
	}
}

func TestRegionalRetiredLeaseQuarantinePublicationAndDrain(t *testing.T) {
	w := regionalWorld(t)
	old, second := w.Acquire(), w.Acquire()
	defer old.Release()
	defer second.Release()
	q := regionalQuery(t, old.Snapshot)
	far := regionalSupport(t, q, geom.Vec3{X: -24, Y: .1})
	farEnd := regionalSupport(t, q, geom.Vec3{X: -20, Y: .1, Z: 2})
	farPath := q.FindPath(context.Background(), far, farEnd, navmesh.Filter{}).Path
	local := regionalSupport(t, q, geom.Vec3{X: 20, Y: .1, Z: 3})
	localEnd := regionalSupport(t, q, geom.Vec3{X: 21, Y: .1, Z: 3})
	localPath := q.FindPath(context.Background(), local, localEnd, navmesh.Filter{}).Path
	if farPath == nil || localPath == nil || w.Safety().HasQuarantine() || !w.Safety().ProtectsRetiredViews() {
		t.Fatal("initial safety/path fixture")
	}
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("right", 20)}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	current := w.Acquire()
	defer current.Release()
	next := regionalQuery(t, current.Snapshot)
	newLocal := regionalSupport(t, next, local.Position())
	newEnd := regionalSupport(t, next, localEnd.Position())
	newPath := next.FindPath(context.Background(), newLocal, newEnd, navmesh.Filter{}).Path
	protected := w.Safety()
	if w.Pending() || protected.CommittedRevision() != protected.DesiredRevision() || !protected.HasQuarantine() {
		t.Fatal("committed retirement quarantine changed desired readiness")
	}
	if !protected.SafeLocation(far) || !protected.SafePath(farPath) || !protected.SafeSegment(far.Position(), farEnd.Position()) {
		t.Fatal("distant old-view movement stopped")
	}
	if protected.SafeLocation(local) || protected.SafePath(localPath) || protected.SafeLocation(newLocal) || protected.SafePath(newPath) || protected.SafeSegment(local.Position(), localEnd.Position()) || protected.SafeBounds(local.Position(), localEnd.Position()) {
		t.Fatal("old or new local view escaped retirement quarantine")
	}
	oldScope, currentScope := protected.ForRevision(old.Snapshot.Revision()), protected.ForRevision(current.Snapshot.Revision())
	if oldScope == nil || currentScope == nil || oldScope.Token() == currentScope.Token() || currentScope.Token() == protected.Token() || oldScope.Token() == protected.Token() {
		t.Fatal("generation scopes lack distinct coherent tokens")
	}
	if !currentScope.SafeLocation(newLocal) || !currentScope.SafePath(newPath) || !currentScope.SafeSegment(local.Position(), localEnd.Position()) || !currentScope.SafeBounds(local.Position(), localEnd.Position()) {
		t.Fatal("latest generation inherited another generation's quarantine")
	}
	if currentScope.SafeLocation(far) || currentScope.SafePath(farPath) || oldScope.SafeLocation(newLocal) || oldScope.SafePath(newPath) {
		t.Fatal("weaker scope accepted another generation's handles")
	}
	if !oldScope.SafeLocation(far) || !oldScope.SafePath(farPath) || oldScope.SafeSegment(local.Position(), localEnd.Position()) || oldScope.SafeBounds(local.Position(), localEnd.Position()) {
		t.Fatal("old generation lost local protection or distant movement")
	}
	stacked := local.Position()
	stacked.Y = 100
	if oldScope.SafeSegment(stacked, stacked) || !currentScope.SafeSegment(stacked, stacked) {
		t.Fatal("scoped quarantine changed across vertical layers")
	}
	if unknown := protected.ForRevision(current.Snapshot.Revision() + 100); unknown.SafeSegment(stacked, stacked) || unknown.SafeLocation(far) || unknown.SafePath(farPath) || unknown.SafeBounds(stacked, stacked) {
		t.Fatal("unknown generation accepted")
	}
	t.Run("ScopeAllocations", func(t *testing.T) {
		if allocs := testing.AllocsPerRun(100, func() {
			if scope := w.Safety().ForRevision(current.Snapshot.Revision()); scope != currentScope || !scope.SafePath(newPath) {
				panic("scope cache changed")
			}
		}); allocs != 0 {
			t.Fatalf("generation scope reads allocate: %g", allocs)
		}
	})
	old.Release()
	if w.Safety() != protected {
		t.Fatal("one of two leases prematurely changed capture")
	}
	second.Release()
	drained := w.Safety()
	if drained.Token() == protected.Token() || drained.HasQuarantine() || !drained.SafeLocation(newLocal) || !drained.SafePath(newPath) || !drained.SafeSegment(newLocal.Position(), newEnd.Position()) || !drained.SafeBounds(newLocal.Position(), newEnd.Position()) {
		t.Fatal("last retired lease did not expose committed geometry")
	}
	if !protected.HasQuarantine() || protected.SafeSegment(newLocal.Position(), newEnd.Position()) {
		t.Fatal("retirement mutated an earlier immutable capture")
	}
	if drained.ForRevision(old.Snapshot.Revision()) != nil || !currentScope.SafeSegment(newLocal.Position(), newEnd.Position()) || oldScope.SafeSegment(newLocal.Position(), newEnd.Position()) {
		t.Fatal("retired scope persisted in new capture or old capture mutated")
	}
	second.Release()
	current.Release()
	if w.Safety() != drained {
		t.Fatal("duplicate or current-generation release changed quarantine token")
	}
}

func TestRegionalRetiredQuarantineTracksEveryPublication(t *testing.T) {
	w := regionalWorld(t)
	old := w.Acquire()
	defer old.Release()
	left, right := geom.Vec3{X: -20, Y: 100}, geom.Vec3{X: 20, Y: 100}
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("right", 20)}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	middle := w.Acquire()
	defer middle.Release()
	first := w.Safety()
	if first.SafeSegment(right, right) || !first.SafeSegment(left, left) {
		t.Fatal("first publication quarantine")
	}
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("left", -20)}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	both := w.Safety()
	if both.SafeSegment(right, right) || both.SafeSegment(left, left) || !first.SafeSegment(left, left) {
		t.Fatal("multiple publications lost history or mutated prior capture")
	}
	oldScope, middleScope, latestScope := both.ForRevision(old.Snapshot.Revision()), both.ForRevision(middle.Snapshot.Revision()), both.ForRevision(w.Snapshot().Revision())
	if oldScope.SafeSegment(right, right) || oldScope.SafeSegment(left, left) || !middleScope.SafeSegment(right, right) || middleScope.SafeSegment(left, left) || !latestScope.SafeSegment(right, right) || !latestScope.SafeSegment(left, left) {
		t.Fatal("generation scopes contain another generation's change history")
	}
	if _, err := w.Prepare(context.Background()); !errors.Is(err, dynamic.ErrBusy) {
		t.Fatal("retired history escaped managed-generation limit", err)
	}
	old.Release()
	onlySecond := w.Safety()
	if onlySecond.Token() == both.Token() || !onlySecond.SafeSegment(right, right) || onlySecond.SafeSegment(left, left) {
		t.Fatal("retiring oldest lease lost newer history or retained obsolete history")
	}
	middle.Release()
	if w.Safety().HasQuarantine() || !w.Safety().SafeSegment(left, left) || !w.Safety().SafeSegment(right, right) {
		t.Fatal("all histories did not drain")
	}
	if _, err := w.Apply(dynamic.Changes{RemoveVolumes: []string{"left", "right"}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	if w.Safety().HasQuarantine() {
		t.Fatal("publication without retained readers created retirement quarantine")
	}
}

func TestRegionalRetiredHistorySurvivesNewerLeaseRelease(t *testing.T) {
	w := regionalWorld(t)
	old := w.Acquire()
	defer old.Release()
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("right", 20)}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	middle := w.Acquire()
	defer middle.Release()
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("left", -20)}}); err != nil {
		t.Fatal(err)
	}
	commit(t, w)
	middle.Release()
	left, right := geom.Vec3{X: -20, Y: 100}, geom.Vec3{X: 20, Y: 100}
	if w.Safety().SafeSegment(left, left) || w.Safety().SafeSegment(right, right) {
		t.Fatal("oldest lease lost changes published after its first retirement")
	}
	if _, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("pending", 0)}}); err != nil {
		t.Fatal(err)
	}
	before := w.Safety()
	old.Release()
	after := w.Safety()
	if after.Token() == before.Token() || !w.Pending() || !after.HasQuarantine() || after.SafeSegment(geom.Vec3{}, geom.Vec3{}) || !after.SafeSegment(left, left) || !after.SafeSegment(right, right) {
		t.Fatal("last retirement cleared pending changes or kept retired columns")
	}
	current := after.ForRevision(w.Snapshot().Revision())
	if current.SafeSegment(geom.Vec3{}, geom.Vec3{}) || !current.SafeSegment(left, left) || !current.SafeSegment(right, right) {
		t.Fatal("latest generation scope lost pending quarantine")
	}
}

func TestRegionalRouteQuarantineIncludesAdjacentTilePadding(t *testing.T) {
	w := regionalWorld(t)
	old := w.Acquire()
	defer old.Release()
	q := regionalQuery(t, old.Snapshot)
	start := regionalSupport(t, q, geom.Vec3{X: 4, Y: .1, Z: 1})
	end := regionalSupport(t, q, geom.Vec3{X: 6, Y: .1, Z: 1})
	path := q.FindPath(context.Background(), start, end, navmesh.Filter{})
	if path.Code != navmesh.Complete || !w.Safety().SafePath(path.Path) {
		t.Fatalf("initial route: %+v", path.Status)
	}
	dirty := artifact.TileKey{X: 2, Z: 0}
	for i := 0; i < path.Path.TileCount(); i++ {
		tile, _ := path.Path.TileAt(i)
		if tile.Key.X == dirty.X && tile.Key.Z == dirty.Z {
			t.Fatal("fixture route enters the dirty tile instead of its padding")
		}
	}
	if _, err := w.SetLoaded([]artifact.TileKey{dirty}, false); err != nil {
		t.Fatal(err)
	}
	safety := w.Safety()
	if !safety.SafeLocation(start) || safety.SafeLocation(end) || safety.SafeSegment(start.Position(), end.Position()) || safety.SafePath(path.Path) {
		t.Fatal("adjacent clean-tile route escaped padded quarantine")
	}
	t.Run("RouteAllocations", func(t *testing.T) {
		if allocs := testing.AllocsPerRun(100, func() { safety.SafePath(path.Path) }); allocs != 0 {
			t.Fatalf("cached padded route check allocated: %g", allocs)
		}
	})
}

func TestRegionalTopologyRejectsReusedFingerprintWithDifferentOrigin(t *testing.T) {
	w := regionalWorld(t)
	lease := w.Acquire()
	defer lease.Release()
	q := regionalQuery(t, lease.Snapshot)
	start := regionalSupport(t, q, geom.Vec3{X: -24, Y: .1})
	end := regionalSupport(t, q, geom.Vec3{X: -20, Y: .1})
	path := q.FindPath(context.Background(), start, end, navmesh.Filter{})
	if path.Code != navmesh.Complete {
		t.Fatal(path.Status)
	}
	bundle, err := lease.Snapshot.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	// Fingerprints are provenance supplied with artifacts, not a substitute
	// for comparing the actual coordinate system of retained handles.
	bundle.Settings.Origin.X += 1000
	bundle.Settings.Bounds.Min.X += 1000
	bundle.Settings.Bounds.Max.X += 1000
	shifted, err := navmesh.NewSnapshot(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if shifted.Topology().SurfaceUnchanged(start) || shifted.Topology().PathUnchanged(path.Path) {
		t.Fatal("retained handles transferred across different coordinate systems")
	}
	shiftedQuery := regionalQuery(t, shifted)
	current := regionalSupport(t, shiftedQuery, geom.Vec3{X: 976, Y: .1})
	if r := shiftedQuery.RepairPath(context.Background(), path.Path, current, navmesh.Filter{}); r.Code != navmesh.Stale {
		t.Fatalf("corridor repaired across different coordinate systems: %+v", r.Status)
	}
}

func TestRegionalSegmentBroadPhaseMatchesExactColumnClipping(t *testing.T) {
	w := regionalWorld(t)
	u, err := w.Apply(dynamic.Changes{Volumes: []geom.Volume{regionalBlocker("right", 20)}})
	if err != nil {
		t.Fatal(err)
	}
	_, config, _ := w.Desired()
	safety := w.Safety()
	exact := func(a, b geom.Vec3) bool {
		for _, key := range u.Dirty {
			box := bake.TileBounds(config, key)
			lo, hi := 0.0, 1.0
			for _, axis := range [][4]float64{{a.X, b.X - a.X, box.Min.X, box.Max.X}, {a.Z, b.Z - a.Z, box.Min.Z, box.Max.Z}} {
				if axis[1] == 0 {
					if axis[0] < axis[2] || axis[0] > axis[3] {
						lo, hi = 1, 0
					}
					continue
				}
				x, y := (axis[2]-axis[0])/axis[1], (axis[3]-axis[0])/axis[1]
				lo, hi = math.Max(lo, math.Min(x, y)), math.Min(hi, math.Max(x, y))
			}
			if lo <= hi {
				return false
			}
		}
		return true
	}
	random := rand.New(rand.NewSource(49))
	for i := 0; i < 2000; i++ {
		a := geom.Vec3{X: random.Float64()*80 - 40, Y: 100, Z: random.Float64()*40 - 20}
		b := geom.Vec3{X: random.Float64()*80 - 40, Y: -100, Z: random.Float64()*40 - 20}
		if got, want := safety.SafeSegment(a, b), exact(a, b); got != want {
			t.Fatalf("segment %v -> %v safe=%v expected=%v", a, b, got, want)
		}
		if safety.SafeSegment(b, a) != exact(b, a) {
			t.Fatal("reverse direction changed clipping")
		}
	}
	for _, key := range u.Dirty {
		b := bake.TileBounds(config, key)
		edge := geom.Vec3{X: b.Min.X, Y: 100, Z: b.Min.Z}
		if safety.SafeSegment(edge, edge) {
			t.Fatal("boundary touch escaped quarantine")
		}
	}
}
