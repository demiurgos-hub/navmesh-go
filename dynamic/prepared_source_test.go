package dynamic

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestPreparedSourceCaptureReusedAcrossPreparationAndStreaming(t *testing.T) {
	scene, cfg := testworld.Scene()
	w, err := New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial := w.geometry
	if initial == nil || initial.SourceFingerprint() != w.source.fingerprint || initial.ResolvedFingerprint() != w.source.resolvedFingerprint {
		t.Fatal("initial source metadata has no matching prepared capture")
	}
	if _, err := w.Apply(Changes{}); err != nil || w.geometry != initial {
		t.Fatal("idempotent edit replaced the prepared capture", err)
	}
	keys, _ := bake.TileKeys(cfg)
	if _, err := w.SetLoaded(keys[:1], false); err != nil || w.geometry != initial {
		t.Fatal("streaming edit replaced source geometry", err)
	}
	candidate, err := w.Prepare(context.Background())
	if err != nil || w.geometry != initial {
		t.Fatal("preparation replaced source geometry", err)
	}
	w.Discard(candidate)
	if _, err := w.Apply(Changes{RemoveInstances: []string{"bridge-instance"}}); err != nil {
		t.Fatal(err)
	}
	accepted := w.geometry
	if accepted == initial || accepted.TriangleCount() >= initial.TriangleCount() || accepted.SourceFingerprint() != w.source.fingerprint {
		t.Fatal("edit did not install one coherent new prepared capture")
	}
	invalid := testworld.Plane("invalid", geom.Vec3{}, geom.Vec3{X: 1, Z: 1})
	invalid.Vertices[0].X = math.NaN()
	if _, err := w.Apply(Changes{Assets: []geom.Asset{invalid}}); err == nil || w.geometry != accepted {
		t.Fatal("failed edit replaced prepared geometry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if candidate, err := w.Prepare(ctx); !errors.Is(err, context.Canceled) || candidate != nil || w.geometry != accepted {
		t.Fatal("cancellation changed desired prepared geometry", err)
	}
	candidate, err = w.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Commit(candidate); err != nil {
		t.Fatal(err)
	}
	bundle, err := w.Snapshot().Bundle()
	if err != nil || bundle.SourceFingerprint != accepted.SourceFingerprint() || bundle.ResolvedFingerprint != accepted.ResolvedFingerprint() || w.geometry != accepted {
		t.Fatal("publication did not use its captured source identity", err)
	}
	// An earlier capture remains independently usable while a worker retains it.
	oldBuild, err := initial.BuildTiles(context.Background(), cfg, nil)
	if err != nil || oldBuild.Bundle.SourceFingerprint == bundle.SourceFingerprint {
		t.Fatal("accepted edit mutated an earlier immutable source capture", err)
	}
}

type pausedPreparedBuildContext struct {
	context.Context
	calls            atomic.Int32
	reached, release chan struct{}
}

func (c *pausedPreparedBuildContext) Err() error {
	if c.calls.Add(1) == 1 {
		close(c.reached)
		<-c.release
	}
	return nil
}

func TestPreparedSourceSupersededCaptureCannotPublish(t *testing.T) {
	scene, cfg := testworld.Scene()
	w, err := New(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := bake.TileKeys(cfg)
	if _, err := w.SetLoaded(keys[:1], false); err != nil {
		t.Fatal(err)
	}
	previous := w.geometry
	ctx := &pausedPreparedBuildContext{Context: context.Background(), reached: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	type result struct {
		candidate *Candidate
		err       error
	}
	finished := make(chan result, 1)
	go func() { candidate, err := w.Prepare(ctx); finished <- result{candidate, err} }()
	select {
	case <-ctx.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not capture desired state")
	}
	if _, err := w.Apply(Changes{RemoveInstances: []string{"bridge-instance"}}); err != nil {
		t.Fatal(err)
	}
	accepted := w.geometry
	if accepted == previous {
		t.Fatal("new edit did not replace desired source capture")
	}
	ctx.release <- struct{}{}
	stale := <-finished
	if !errors.Is(stale.err, ErrSuperseded) || stale.candidate != nil || w.geometry != accepted {
		t.Fatal("superseded preparation replaced desired source", stale.err)
	}
	current, err := w.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Commit(current); err != nil {
		t.Fatal(err)
	}
	bundle, err := w.Snapshot().Bundle()
	if err != nil || bundle.SourceFingerprint != accepted.SourceFingerprint() {
		t.Fatal("fresh preparation published stale geometry", err)
	}
}
