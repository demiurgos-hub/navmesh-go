// Package dynamic retains generic source geometry and prepares transactional
// replacement snapshots. It owns no engine workers, ticks, entities, or disk IO.
package dynamic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

var (
	// ErrSuperseded means desired geometry or the base generation changed.
	ErrSuperseded = errors.New("dynamic: candidate superseded")
	// ErrBusy means an existing preparation or retained generation fills capacity.
	ErrBusy = errors.New("dynamic: build/snapshot capacity exhausted")
)

// State describes an update lifecycle independently of its committed mesh.
type State string

const (
	Pending    State = "pending"
	Building   State = "building"
	Prepared   State = "prepared"
	Committed  State = "committed"
	Failed     State = "failed"
	Superseded State = "superseded"
	Cancelled  State = "cancelled"
)

// Update identifies a desired-state revision and its current status.
type Update struct {
	Revision uint64
	State    State
	Dirty    []artifact.TileKey
	Err      error
}

// Changes updates independent source objects. Empty IDs and missing removals
// are rejected; removing an object never removes its shared asset or terrain.
// Revision zero requests automatic increment; explicit revisions must advance
// when behavior changes. Repeating an unchanged object is idempotent.
type Changes struct {
	Assets                                                    []geom.Asset
	Instances                                                 []geom.Instance
	Volumes                                                   []geom.Volume
	Links                                                     []geom.Link
	RemoveAssets, RemoveInstances, RemoveVolumes, RemoveLinks []string
}

// World is a concurrency-safe desired-state registry and snapshot publisher.
// Call Prepare on a host-owned background worker, Commit on its publication
// thread, and Acquire for concurrent readers. No background work is implicit.
type World struct {
	mu          sync.Mutex
	scene       geom.Scene
	config      bake.Config
	revision    uint64
	current     *navmesh.Snapshot
	dirty       map[artifact.TileKey]bool
	state       State
	lastError   error
	building    bool
	prepared    *Candidate
	leases      map[*navmesh.Snapshot]int
	unavailable map[artifact.TileKey]bool
}

// New bakes and validates initial source geometry synchronously.
func New(ctx context.Context, scene geom.Scene, config bake.Config) (*World, error) {
	scene = scene.Clone()
	if err := normalizeRevisions(geom.Scene{}, &scene); err != nil {
		return nil, err
	}
	r, err := bake.Build(ctx, scene, config)
	if err != nil {
		return nil, err
	}
	snapshot, err := navmesh.NewSnapshot(r.Bundle)
	if err != nil {
		return nil, err
	}
	return &World{scene: scene.Clone(), config: config, revision: snapshot.Revision(), current: snapshot, dirty: map[artifact.TileKey]bool{}, state: Committed, leases: map[*navmesh.Snapshot]int{}, unavailable: map[artifact.TileKey]bool{}}, nil
}

// Snapshot returns the immutable current snapshot. Managed hosts should Acquire
// a lease instead so retention limits account for their long-running readers.
func (w *World) Snapshot() *navmesh.Snapshot { w.mu.Lock(); defer w.mu.Unlock(); return w.current }

// Pending is a coherent constant-time readiness check. It does not copy/sort
// dirty tiles or source geometry, and is suitable for a host's tick hot path.
func (w *World) Pending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.revision != w.current.Revision()
}

// Status returns a copied update status, including quarantined dirty columns.
func (w *World) Status() Update {
	w.mu.Lock()
	u := Update{w.revision, w.state, keySlice(w.dirty), w.lastError}
	w.mu.Unlock()
	sortKeys(u.Dirty)
	return u
}

// Desired returns a deep copy for checkpointing; runtime references are omitted.
func (w *World) Desired() (geom.Scene, bake.Config, uint64) {
	w.mu.Lock()
	c := w.config
	c.Revision = w.revision
	scene, revision := w.scene, w.revision
	w.mu.Unlock()
	return scene.Clone(), c, revision
}

// Apply validates a complete desired-state mutation before accepting it.
// Failed builds retain these sources and dirty columns for an explicit retry.
// Validation may inspect the full source scene; run large changes off the tick.
// It never holds the publication lock during geometry resolution. Concurrent
// desired-state edits may return ErrSuperseded and should be recaptured/retried.
func (w *World) Apply(ch Changes) (Update, error) {
	w.mu.Lock()
	previous, config, revision := w.scene, w.config, w.revision
	w.mu.Unlock()
	next := previous.Clone()
	var err error
	next.Assets, err = merge(next.Assets, ch.Assets, ch.RemoveAssets, func(v geom.Asset) string { return v.ID })
	if err != nil {
		return Update{}, err
	}
	next.Instances, err = merge(next.Instances, ch.Instances, ch.RemoveInstances, func(v geom.Instance) string { return v.ID })
	if err != nil {
		return Update{}, err
	}
	next.Volumes, err = merge(next.Volumes, ch.Volumes, ch.RemoveVolumes, func(v geom.Volume) string { return v.ID })
	if err != nil {
		return Update{}, err
	}
	next.Links, err = merge(next.Links, ch.Links, ch.RemoveLinks, func(v geom.Link) string { return v.ID })
	if err != nil {
		return Update{}, err
	}
	if err = normalizeRevisions(previous, &next); err != nil {
		return Update{}, err
	}
	// Own every upsert slice before publishing or resolving it.
	next = next.Clone()
	if _, err = next.Resolve(config.Settings.Origin, config.Strict); err != nil {
		return Update{}, err
	}
	oldHash, _ := previous.Fingerprint()
	newHash, _ := next.Fingerprint()
	if oldHash == newHash {
		w.mu.Lock()
		if w.revision != revision {
			w.mu.Unlock()
			return Update{}, ErrSuperseded
		}
		u := Update{w.revision, w.state, keySlice(w.dirty), w.lastError}
		w.mu.Unlock()
		sortKeys(u.Dirty)
		return u, nil
	}
	if revision == ^uint64(0) {
		return Update{}, errors.New("dynamic: revision exhausted")
	}
	boxes, err := changedBounds(previous, next, config.Profile.Height)
	if err != nil {
		return Update{}, err
	}
	keys, err := bake.TileKeys(config)
	if err != nil {
		return Update{}, err
	}
	dirty := make([]artifact.TileKey, 0)
	for _, key := range keys {
		for _, box := range boxes {
			if bake.TileBounds(config, key).Intersects(box) {
				dirty = append(dirty, key)
				break
			}
		}
	}
	w.mu.Lock()
	if w.revision != revision {
		w.mu.Unlock()
		return Update{}, ErrSuperseded
	}
	for _, key := range dirty {
		w.dirty[key] = true
	}
	w.scene = next
	w.revision++
	w.state = Pending
	w.lastError = nil
	if w.prepared != nil {
		w.prepared = nil
	}
	u := Update{w.revision, w.state, keySlice(w.dirty), nil}
	w.mu.Unlock()
	sortKeys(u.Dirty)
	return u, nil
}
func merge[T any](old, upserts []T, removes []string, id func(T) string) ([]T, error) {
	m := map[string]T{}
	for _, v := range old {
		m[id(v)] = v
	}
	seen := map[string]bool{}
	for _, key := range removes {
		if key == "" || seen[key] {
			return nil, errors.New("dynamic: duplicate/empty removal")
		}
		seen[key] = true
		if _, ok := m[key]; !ok {
			return nil, fmt.Errorf("dynamic: missing removal %q", key)
		}
		delete(m, key)
	}
	for _, v := range upserts {
		key := id(v)
		if key == "" || seen[key] {
			return nil, errors.New("dynamic: duplicate/conflicting change")
		}
		seen[key] = true
		m[key] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out, nil
}
func changedBounds(a, b geom.Scene, agentHeight float64) ([]geom.Bounds, error) {
	type object struct {
		data string
		box  geom.Bounds
	}
	objects := func(s geom.Scene) (map[string]object, error) {
		r, e := s.Resolve(geom.Vec3{}, false)
		if e != nil {
			return nil, e
		}
		out := map[string]object{}
		groups := map[string][]geom.ResolvedTriangle{}
		for _, t := range r.Triangles {
			groups[t.ObjectID] = append(groups[t.ObjectID], t)
		}
		for id, ts := range groups {
			box := ts[0].Bounds
			for _, t := range ts[1:] {
				box = box.Union(t.Bounds)
			}
			data, _ := json.Marshal(ts)
			out["object:"+id] = object{string(data), box}
		}
		for _, v := range s.Volumes {
			data, _ := json.Marshal(v)
			box := v.Bounds
			if v.Exclude {
				box.Min.Y -= agentHeight
			}
			out["volume:"+v.ID] = object{string(data), box}
		}
		for _, l := range s.Links {
			data, _ := json.Marshal(l)
			box := geom.Bounds{Min: l.Start, Max: l.Start}.Union(geom.Bounds{Min: l.End, Max: l.End}).Expand(geom.Vec3{X: l.Radius, Y: l.Radius, Z: l.Radius})
			out["link:"+l.ID] = object{string(data), box}
		}
		return out, nil
	}
	x, err := objects(a)
	if err != nil {
		return nil, err
	}
	y, err := objects(b)
	if err != nil {
		return nil, err
	}
	var boxes []geom.Bounds
	for id, o := range x {
		n, ok := y[id]
		if !ok || o.data != n.data {
			boxes = append(boxes, o.box)
			if ok {
				boxes = append(boxes, n.box)
			}
		}
	}
	for id, n := range y {
		if _, ok := x[id]; !ok {
			boxes = append(boxes, n.box)
		}
	}
	return boxes, nil
}
func sortedKeys(m map[artifact.TileKey]bool) []artifact.TileKey {
	out := keySlice(m)
	sortKeys(out)
	return out
}
func keySlice(m map[artifact.TileKey]bool) []artifact.TileKey {
	out := make([]artifact.TileKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
func sortKeys(out []artifact.TileKey) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		if out[i].Z != out[j].Z {
			return out[i].Z < out[j].Z
		}
		return out[i].Layer < out[j].Layer
	})
}

// Candidate is a fully assembled replacement tied to its desired/base versions.
// Its fields are private so callers cannot mutate a prepared transaction.
type Candidate struct {
	world          *World
	revision, base uint64
	snapshot       *navmesh.Snapshot
	dirty          []artifact.TileKey
}

// Revision identifies the candidate's captured desired state.
func (c *Candidate) Revision() uint64 {
	if c == nil {
		return 0
	}
	return c.revision
}

// Prepare rebuilds affected tiles and assembles a completely independent mesh.
// It does not publish. At most three managed generations including staging are
// permitted; old leased snapshots are never modified or freed by publication.
func (w *World) Prepare(ctx context.Context) (*Candidate, error) {
	w.mu.Lock()
	if w.building || w.prepared != nil {
		w.mu.Unlock()
		return nil, ErrBusy
	}
	retained := 1
	for s, n := range w.leases {
		if s != w.current && n > 0 {
			retained++
		}
	}
	if retained >= 3 {
		w.mu.Unlock()
		return nil, ErrBusy
	}
	// Source captures are immutable: Apply replaces complete owned scenes.
	scene, cfg, revision, base := w.scene, w.config, w.revision, w.current
	keys := keySlice(w.dirty)
	unavailable := map[artifact.TileKey]bool{}
	for k, v := range w.unavailable {
		unavailable[k] = v
	}
	cfg.Revision = revision
	w.building = true
	w.state = Building
	w.mu.Unlock()
	sortKeys(keys)
	fail := func(err error) (*Candidate, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.building = false
		if w.revision == revision {
			w.state = Failed
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				w.state = Cancelled
			}
			w.lastError = err
		}
		return nil, err
	}
	b, err := base.Bundle()
	if err != nil {
		return fail(err)
	}
	if ctx == nil {
		return fail(errors.New("dynamic: nil context"))
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	if len(keys) > 0 {
		result, e := bake.BuildTiles(ctx, scene, cfg, keys)
		if e != nil {
			return fail(e)
		}
		replacement := map[artifact.TileKey]artifact.Tile{}
		for _, t := range result.Bundle.Tiles {
			replacement[t.Key] = t
		}
		var tiles []artifact.Tile
		for _, t := range b.Tiles {
			column := artifact.TileKey{X: t.Key.X, Z: t.Key.Z}
			if _, changed := replacement[column]; !changed {
				tiles = append(tiles, t)
			}
		}
		tiles = append(tiles, result.Bundle.Tiles...)
		b = result.Bundle
		b.Tiles = tiles
	} else {
		b.Revision = revision
		resolved, e := scene.Resolve(cfg.Settings.Origin, cfg.Strict)
		if e != nil {
			return fail(e)
		}
		b.SourceFingerprint, b.ResolvedFingerprint = resolved.Fingerprint, resolved.ResolvedFingerprint
		b.Links = append([]geom.Link(nil), scene.Links...)
	}
	filtered := b.Tiles[:0]
	for _, t := range b.Tiles {
		if !unavailable[artifact.TileKey{X: t.Key.X, Z: t.Key.Z}] {
			filtered = append(filtered, t)
		}
	}
	b.Tiles = filtered
	b.Revision = revision
	snapshot, err := navmesh.NewSnapshot(b)
	if err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	c := &Candidate{w, revision, base.Revision(), snapshot, keys}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.building = false
	if w.revision != revision || w.current != base {
		return nil, ErrSuperseded
	}
	w.prepared = c
	w.state = Prepared
	return c, nil
}

// Commit atomically publishes a prepared candidate if all captured revisions
// remain current. This performs no baking, mesh assembly, or disk IO.
func (w *World) Commit(c *Candidate) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c == nil || c.world != w || w.prepared != c || c.revision != w.revision || c.base != w.current.Revision() {
		return ErrSuperseded
	}
	old := w.current
	w.current = c.snapshot
	w.prepared = nil
	w.dirty = map[artifact.TileKey]bool{}
	w.state = Committed
	w.lastError = nil
	if w.leases[old] == 0 {
		delete(w.leases, old)
	}
	return nil
}

// Discard releases a prepared candidate without dropping pending geometry.
func (w *World) Discard(c *Candidate) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.prepared == c {
		w.prepared = nil
		w.state = Pending
	}
}

// SetLoaded changes streaming intent. Reload rebuilds from retained sources;
// unload is published through the same transaction as geometry changes.
func (w *World) SetLoaded(keys []artifact.TileKey, loaded bool) (Update, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	valid, err := bake.TileKeys(w.config)
	if err != nil {
		return Update{}, err
	}
	set := map[artifact.TileKey]bool{}
	for _, k := range valid {
		set[k] = true
	}
	for _, k := range keys {
		if !set[k] {
			return Update{}, fmt.Errorf("dynamic: unknown tile %v", k)
		}
	}
	if w.revision == ^uint64(0) {
		return Update{}, errors.New("dynamic: revision exhausted")
	}
	for _, k := range keys {
		if loaded {
			delete(w.unavailable, k)
		} else {
			w.unavailable[k] = true
		}
		w.dirty[k] = true
	}
	w.revision++
	w.prepared = nil
	w.state = Pending
	return Update{w.revision, w.state, sortedKeys(w.dirty), nil}, nil
}

// Lease retains a managed snapshot until Release. Do not retain its query or
// surfaces after release. Release is concurrency-safe and idempotent.
type Lease struct {
	Snapshot *navmesh.Snapshot
	world    *World
	once     sync.Once
}

// Acquire retains the current committed snapshot.
func (w *World) Acquire() *Lease {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.leases[w.current]++
	return &Lease{Snapshot: w.current, world: w}
}

// Release ends the reader's retention period.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		w := l.world
		w.mu.Lock()
		defer w.mu.Unlock()
		w.leases[l.Snapshot]--
		if w.leases[l.Snapshot] == 0 {
			delete(w.leases, l.Snapshot)
		}
	})
}
