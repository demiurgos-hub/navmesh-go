package artifact_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
	"github.com/demiurgos-hub/navmesh-go/internal/testworld"
)

func TestTypedCloneMatchesCodecAndOwnsNestedValues(t *testing.T) {
	scene, config := testworld.Scene()
	config.Settings.BuildLayers = true
	scene.Links = []geom.Link{{ID: "stairs", Start: geom.Vec3{X: -4, Y: .1}, End: geom.Vec3{X: -4, Y: 3.1}, Radius: .4, Flags: 1, Profiles: []string{"z", "human"}}}
	r, err := bake.Build(context.Background(), scene, config)
	if err != nil {
		t.Fatal(err)
	}
	b := r.Bundle
	for i, j := 0, len(b.Tiles)-1; i < j; i, j = i+1, j-1 {
		b.Tiles[i], b.Tiles[j] = b.Tiles[j], b.Tiles[i]
	}
	data, err := artifact.Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	want, err := artifact.Decode(data, artifact.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("typed clone differs from canonical round trip")
	}
	mutateCloneSlices(reflect.ValueOf(&b).Elem(), false)
	after, err := artifact.Encode(got)
	if err != nil {
		t.Fatal("source mutations reached clone", err)
	}
	if !bytes.Equal(after, data) {
		t.Fatal("source nested arrays alias clone")
	}
	// Empty tiles omit all construction parameters on the portable wire.
	want.Tiles[0].Empty = true
	want.Tiles[0].Parameters.PolyCount = 0
	want.Tiles[0].Parameters.UserId = 123
	got, err = want.Clone()
	if err != nil {
		t.Fatal(err)
	}
	for _, tile := range got.Tiles {
		if tile.Key == want.Tiles[0].Key && !reflect.ValueOf(tile.Parameters).IsZero() {
			t.Fatal("empty tile parameters were not normalized")
		}
	}
}

func mutateCloneSlices(v reflect.Value, owned bool) {
	switch v.Kind() {
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutateCloneSlices(v.Index(i), true)
		}
	case reflect.Struct, reflect.Array:
		count := 0
		if v.Kind() == reflect.Array {
			count = v.Len()
		} else {
			count = v.NumField()
		}
		for i := 0; i < count; i++ {
			if v.Kind() == reflect.Array {
				mutateCloneSlices(v.Index(i), owned)
			} else {
				mutateCloneSlices(v.Field(i), owned)
			}
		}
	default:
		if !owned || !v.CanSet() {
			return
		}
		switch v.Kind() {
		case reflect.String:
			v.SetString(v.String() + "changed")
		case reflect.Bool:
			v.SetBool(!v.Bool())
		case reflect.Float32, reflect.Float64:
			v.SetFloat(v.Float() + 1)
		case reflect.Int, reflect.Int32, reflect.Int64:
			v.SetInt(v.Int() + 1)
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			v.SetUint(v.Uint() + 1)
		}
	}
}
