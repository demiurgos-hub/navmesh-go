package dynamic_test

import (
	"context"
	"errors"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/dynamic"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

// FuzzWorldUpdates explores bounded desired/committed sequences, not just an
// isolated geometric answer. Inputs are intentionally short to bound bake work.
func FuzzWorldUpdates(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{2, 2, 0, 4, 1, 3})
	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 12 {
			return
		}
		scene, cfg := testworld.Scene()
		w, e := dynamic.New(context.Background(), scene, cfg)
		if e != nil {
			t.Fatal(e)
		}
		bridge, blocker := true, false
		keys, e := bake.TileKeys(cfg)
		if e != nil {
			t.Fatal(e)
		}
		for _, op := range ops {
			before := w.Snapshot()
			switch op % 8 {
			case 0:
				if bridge {
					_, e = w.Apply(dynamic.Changes{RemoveInstances: []string{"bridge-instance"}})
				} else {
					_, e = w.Apply(dynamic.Changes{Instances: []geom.Instance{scene.Instances[1]}})
				}
				bridge = !bridge
			case 1:
				if blocker {
					_, e = w.Apply(dynamic.Changes{RemoveVolumes: []string{"block"}})
				} else {
					_, e = w.Apply(dynamic.Changes{Volumes: []geom.Volume{{ID: "block", Shape: "box", Exclude: true, Bounds: geom.Bounds{Min: geom.Vec3{X: -1, Z: -1}, Max: geom.Vec3{X: 1, Y: 2, Z: 1}}}}})
				}
				blocker = !blocker
			case 2:
				c, cancel := context.WithCancel(context.Background())
				cancel()
				_, e = w.Prepare(c)
				if !errors.Is(e, context.Canceled) || w.Snapshot() != before {
					t.Fatal("cancel published", e)
				}
				e = nil
			case 3, 4:
				_, e = w.SetLoaded(keys, op%8 == 4)
			case 5:
				data, err := w.Checkpoint("fuzz")
				if err != nil {
					t.Fatal(err)
				}
				old, _, _ := w.Desired()
				want, err := old.Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				w, e = dynamic.Restore(context.Background(), data, "fuzz")
				if e == nil {
					restored, _, _ := w.Desired()
					got, err := restored.Fingerprint()
					if err != nil || got != want {
						t.Fatal("restart changed desired sources", err)
					}
				}
			default:
				c, err := w.Prepare(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if op%8 == 6 {
					w.Discard(c)
					if w.Snapshot() != before {
						t.Fatal("discard published")
					}
				} else {
					e = w.Commit(c)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if w.Snapshot().Revision() > w.Status().Revision {
				t.Fatal("committed future revision")
			}
		}
		candidate, e := w.Prepare(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if e = w.Commit(candidate); e != nil {
			t.Fatal(e)
		}
		if w.Snapshot().Revision() != w.Status().Revision {
			t.Fatal("final desired state not committed")
		}
	})
}
