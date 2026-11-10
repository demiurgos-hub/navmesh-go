package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestSourceToArtifactAndFailedWritePreservesPrevious(t *testing.T) {
	scene, cfg := testworld.Scene()
	doc, e := json.Marshal(request{Generator: "Golem Scribe navigation source v1", Scene: scene, Config: cfg})
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "source.json"), filepath.Join(dir, "world.nav")
	if e = os.WriteFile(in, doc, 0600); e != nil {
		t.Fatal(e)
	}
	if e = run(in, out); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	b, e := artifact.Decode(before, artifact.DefaultLimits())
	if e != nil || len(b.Tiles) != 16 {
		t.Fatal("bad CLI artifact", e)
	}
	if e = os.WriteFile(in, []byte(`{"Unknown":1}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e = run(in, out); e == nil {
		t.Fatal("unknown source field accepted")
	}
	after, e := os.ReadFile(out)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("failed build replaced previous artifact", e)
	}
}
