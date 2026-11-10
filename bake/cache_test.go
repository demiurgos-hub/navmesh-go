package bake_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
	"github.com/demiurgos-hub/navmesh-go/loader"
	dtcache "github.com/demiurgos-hub/navmesh-go/tilecache"
)

func TestCompressedLayersRoundTrip(t *testing.T) {
	scene, cfg := testworld.Scene()
	cfg.Settings.BuildLayers = true
	r, err := bake.Build(context.Background(), scene, cfg)
	if err != nil {
		t.Fatal(err)
	}
	layers, err := bake.CompressLayers(r.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) == 0 {
		t.Fatal("no layer output")
	}
	index := 0
	for _, tile := range r.Bundle.Tiles {
		for _, want := range tile.Layers {
			got := layers[index]
			index++
			var decoded *dtcache.DtTileCacheLayer
			st := dtcache.DtDecompressTileCacheLayer(&loader.FastLZCompressor{}, got.Data, int32(len(got.Data)), &decoded)
			if detour.DtStatusFailed(st) {
				t.Fatalf("decompress %#x", st)
			}
			if !bytes.Equal(decoded.Heights, want.Heights) || !bytes.Equal(decoded.Areas, want.Areas) || !bytes.Equal(decoded.Cons, want.Connections) || decoded.Header.Tx != tile.Key.X || decoded.Header.Ty != tile.Key.Z {
				t.Fatal("layer content or identity changed")
			}
		}
	}
}
