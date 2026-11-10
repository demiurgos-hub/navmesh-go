package debugger_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	navmesh "github.com/demiurgos-hub/navmesh-go"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/debugger"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestCopiedDebugViews(t *testing.T) {
	scene, cfg := testworld.Scene()
	cfg.WorldID = "<world>"
	r, e := bake.Build(context.Background(), scene, cfg)
	if e != nil {
		t.Fatal(e)
	}
	s, e := navmesh.NewSnapshot(r.Bundle)
	if e != nil {
		t.Fatal(e)
	}
	report, e := debugger.Capture(s, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Polygons) == 0 {
		t.Fatal("no debug geometry")
	}
	for _, side := range []bool{false, true} {
		var b bytes.Buffer
		if e = report.WriteSVG(&b, side); e != nil {
			t.Fatal(e)
		}
		if strings.Contains(b.String(), "<world>") {
			t.Fatal("source label not escaped")
		}
		dec := xml.NewDecoder(&b)
		for {
			_, e = dec.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	var b bytes.Buffer
	if e = report.WriteJSON(&b); e != nil {
		t.Fatal(e)
	}
	report.Polygons[0].Vertices[0].Y = 99
	other, e := debugger.Capture(s, nil, nil)
	if e != nil || other.Polygons[0].Vertices[0].Y == 99 {
		t.Fatal("debugger aliases snapshot", e)
	}
}
