package artifact_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

func TestClearancePrecisionBoundaries(t *testing.T) {
	s := artifact.Settings{CellSize: float64(float32(.1)), CellHeight: float64(float32(.1)), ClearancePrecision: artifact.ClearanceBinary32}
	for _, test := range []struct {
		name     string
		value    float32
		up, down int
	}{
		{"below", math.Nextafter32(.5, 0), 5, 4},
		{"exact", .5, 5, 5},
		{"above", math.Nextafter32(.5, 1), 6, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := float64(test.value)
			q, err := s.QuantizeClearance(geom.Profile{Height: v, Radius: v, MaxClimb: v})
			if err != nil || q.Height != test.up || q.Radius != test.up || q.Climb != test.down {
				t.Fatalf("quantized=%+v error=%v", q, err)
			}
		})
	}
	s.ClearancePrecision = artifact.ClearanceBinary64
	q, err := s.QuantizeClearance(geom.Profile{Height: .5, Radius: .5, MaxClimb: .5})
	if err != nil || q.Climb != 4 {
		t.Fatalf("default changed widened-binary32 input semantics: %+v %v", q, err)
	}
	s.CellSize, s.CellHeight = .25, .25
	for _, v := range []float64{math.Nextafter(.5, 0), .5, math.Nextafter(.5, 1)} {
		q, err := s.QuantizeClearance(geom.Profile{Height: v, Radius: v, MaxClimb: v})
		if err != nil || q.Height != int(math.Ceil(v/.25)) || q.Radius != int(math.Ceil(v/.25)) || q.Climb != int(math.Floor(v/.25)) {
			t.Fatalf("binary64 neighbor changed: %.17g %+v %v", v, q, err)
		}
	}
	s.CellSize, s.CellHeight = .1, .1
	value := .3
	legacy, err := s.QuantizeClearance(geom.Profile{Height: value, Radius: value, MaxClimb: value})
	if err != nil || legacy.Climb != 2 {
		t.Fatalf("binary64 decimal ratio changed: %+v %v", legacy, err)
	}
	s.ClearancePrecision = artifact.ClearanceBinary32
	native, err := s.QuantizeClearance(geom.Profile{Height: value, Radius: value, MaxClimb: value})
	if err != nil || native != (artifact.VoxelClearance{Height: 3, Radius: 3, Climb: 3}) {
		t.Fatalf("binary32 decimal ratio: %+v %v", native, err)
	}
}

func TestClearancePrecisionRejectsInvalidAndOverflowingDimensions(t *testing.T) {
	for _, change := range []string{"mode", "cell infinity", "cell underflow", "height overflow", "height underflow", "radius infinity", "radius underflow", "climb NaN", "ratio overflow", "height capacity", "radius capacity", "climb capacity"} {
		t.Run(change, func(t *testing.T) {
			s := artifact.Settings{CellSize: .1, CellHeight: .1, ClearancePrecision: artifact.ClearanceBinary32}
			p := geom.Profile{Height: 2, Radius: .2, MaxClimb: .5}
			switch change {
			case "mode":
				s.ClearancePrecision = "guess"
			case "cell infinity":
				s.CellHeight = math.Inf(1)
			case "cell underflow":
				s.CellHeight = math.SmallestNonzeroFloat64
			case "height overflow":
				p.Height = math.MaxFloat64
			case "height underflow":
				p.Height = math.SmallestNonzeroFloat64
			case "radius infinity":
				p.Radius = math.Inf(1)
			case "radius underflow":
				p.Radius = math.SmallestNonzeroFloat64
			case "climb NaN":
				p.MaxClimb = math.NaN()
			case "ratio overflow":
				s.CellHeight = math.SmallestNonzeroFloat32
				p.Height = math.MaxFloat32
			case "height capacity":
				p.Height = 25.6
			case "radius capacity":
				p.Radius = 12.8
			case "climb capacity":
				s.CellHeight, p.MaxClimb = .25, 2047.75
			}
			if _, err := s.QuantizeClearance(p); err == nil {
				t.Fatal("invalid precision or clearance accepted")
			}
		})
	}
}

func TestDefaultClearanceFingerprintAndArtifactBytesRemainCompatible(t *testing.T) {
	c := bake.DefaultConfig("precision", geom.Bounds{Min: geom.Vec3{X: -4, Y: -1, Z: -4}, Max: geom.Vec3{X: 4, Y: 3, Z: 4}})
	fingerprint, err := c.Fingerprint()
	if err != nil || fingerprint != "fb66666a4a053b59c99e06ebaaf583f87efde4ad4b702e34bf268064de08d0eb" {
		t.Fatalf("default fingerprint changed: %s %v", fingerprint, err)
	}
	result, err := bake.Build(context.Background(), geom.Scene{}, c)
	if err != nil {
		t.Fatal(err)
	}
	// The layout is compared under the producer's resolved-fingerprint
	// definition, which hashed this empty scene's canonical JSON.
	legacy := sha256.Sum256([]byte(`{"Triangles":null,"Volumes":null,"Links":null}`))
	result.Bundle.ResolvedFingerprint = fmt.Sprintf("%x", legacy)
	encoded, err := artifact.Encode(result.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	// Captured from the published binary64 producer 26709a54ae426e4d5eb30999d6880e790674e629.
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != "ce27f67b660ce3f5d71536c5292c93e0e6b3529b24a1f246e28fcde361c6f225" {
		t.Fatalf("default artifact changed: %s", got)
	}
	c.Settings.ClearancePrecision = artifact.ClearanceBinary32
	selected, err := c.Fingerprint()
	if err != nil || selected == fingerprint {
		t.Fatalf("precision policy absent from fingerprint: %s %v", selected, err)
	}
	result, err = bake.Build(context.Background(), geom.Scene{}, c)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = artifact.Encode(result.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(encoded[12:16]) != 1 {
		t.Fatal("binary32 artifact lacks required policy feature")
	}
	decoded, err := artifact.Decode(encoded, artifact.DefaultLimits())
	if err != nil || decoded.Settings.ClearancePrecision != artifact.ClearanceBinary32 {
		t.Fatalf("lost clearance policy: %v", err)
	}
	binary.LittleEndian.PutUint32(encoded[12:16], 2)
	hash := sha256.Sum256(encoded[:len(encoded)-32])
	copy(encoded[len(encoded)-32:], hash[:])
	if _, err = artifact.Decode(encoded, artifact.DefaultLimits()); err == nil {
		t.Fatal("unknown required feature accepted")
	}
}
