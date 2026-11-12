package artifact

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"strings"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func filledCloneParameters() detour.DtNavMeshCreateParams {
	var p detour.DtNavMeshCreateParams
	v := reflect.ValueOf(&p).Elem()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.Slice {
			v.Field(i).Set(reflect.MakeSlice(v.Field(i).Type(), 2, 2))
		}
	}
	return p
}

func TestCloneOwnsEveryConstructionArray(t *testing.T) {
	p := filledCloneParameters()
	copy := cloneParameters(p)
	a, b := reflect.ValueOf(&p).Elem(), reflect.ValueOf(&copy).Elem()
	for i := 0; i < a.NumField(); i++ {
		if a.Field(i).Kind() != reflect.Slice {
			continue
		}
		x, y := a.Field(i), b.Field(i)
		if x.Index(0).Kind() == reflect.Float32 {
			x.Index(0).SetFloat(3)
			y.Index(1).SetFloat(7)
		} else {
			x.Index(0).SetUint(3)
			y.Index(1).SetUint(7)
		}
		if !y.Index(0).IsZero() || !x.Index(1).IsZero() {
			t.Fatalf("construction array %s aliases", a.Type().Field(i).Name)
		}
	}
}

func TestCloneLimitAccountingMatchesPortableWire(t *testing.T) {
	b := Bundle{WorldID: "world", Tiles: []Tile{{Parameters: filledCloneParameters(), Layers: []Layer{{Heights: []uint8{1, 2, 3}, Areas: []uint8{1, 2, 3}, Connections: []uint8{1, 2, 3}}}}}, Links: []geom.Link{{ID: "link", Action: "action", Profiles: []string{"z", "human"}}}}
	c := codec{w: new(bytes.Buffer)}
	c.w.WriteString(magic)
	major, minor, features := uint16(1), uint16(0), uint32(0)
	c.value(&major)
	c.value(&minor)
	c.value(&features)
	upstream, producer := UpstreamRevision, ProducerVersion
	c.str(&upstream)
	c.str(&producer)
	c.bundle(&b, DefaultLimits())
	if c.err != nil {
		t.Fatal(c.err)
	}
	limits := DefaultLimits()
	limits.MaxBytes = uint64(c.w.Len() + sha256.Size)
	limits.MaxElements = 40
	if err := cloneLimitsWith(b, limits); err != nil {
		t.Fatal("exact wire limits rejected", err)
	}
	limits.MaxBytes--
	if cloneLimitsWith(b, limits) == nil {
		t.Fatal("wire byte limit ignored")
	}
	limits = DefaultLimits()
	limits.MaxElements = 39
	if cloneLimitsWith(b, limits) == nil {
		t.Fatal("aggregate element limit ignored")
	}
	b.Links[0].Action = strings.Repeat("x", (1<<20)+1)
	if cloneLimits(b) == nil {
		t.Fatal("string limit ignored")
	}
}
