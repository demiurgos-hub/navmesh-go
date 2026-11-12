package dynamic

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// Metadata belongs to one immutable desired source capture. Geometry signatures
// use resolved vertex/area/order identity, so revision-only instance updates and
// unused asset edits retain the same dirty-region behavior as source resolution.
type sourceMetadata struct {
	fingerprint         string
	resolvedFingerprint string
	objects             map[string]sourceObject
}
type sourceObject struct {
	signature [32]byte
	bounds    geom.Bounds
}

func sourceState(scene geom.Scene, resolved *bake.PreparedSource, origin geom.Vec3, region geom.Bounds, agentHeight float64) sourceMetadata {
	out := sourceMetadata{fingerprint: resolved.SourceFingerprint(), resolvedFingerprint: resolved.ResolvedFingerprint(), objects: make(map[string]sourceObject, len(scene.Instances)+len(scene.Volumes)+len(scene.Links))}
	var record [113]byte
	var lastID, key string
	for i := 0; i < resolved.TriangleCount(); i++ {
		triangle, _ := resolved.TriangleAt(i)
		if triangle.ObjectID != lastID {
			lastID, key = triangle.ObjectID, "object:"+triangle.ObjectID
		}
		value, exists := out.objects[key]
		if !exists {
			value.bounds = triangle.Bounds
		} else {
			value.bounds = value.bounds.Union(triangle.Bounds)
		}
		copy(record[:32], value.signature[:])
		at := 32
		for _, p := range triangle.Vertices {
			for _, v := range [3]float64{p.X, p.Y, p.Z} {
				binary.LittleEndian.PutUint64(record[at:at+8], math.Float64bits(v))
				at += 8
			}
		}
		record[104] = triangle.Area
		binary.LittleEndian.PutUint64(record[105:], uint64(triangle.SourceTriangle))
		value.signature = sha256.Sum256(record[:])
		out.objects[key] = value
	}
	for key, value := range out.objects {
		value.bounds = sourceWorldBounds(value.bounds, origin)
		out.objects[key] = value
	}
	// Resolve has already validated the full scene and its JSON fingerprint;
	// these small value records cannot introduce a serialization error.
	for _, volume := range scene.Volumes {
		data, _ := json.Marshal(volume)
		bounds := volume.Bounds
		if volume.Exclude {
			bounds.Min.Y -= agentHeight
		}
		out.objects["volume:"+volume.ID] = sourceObject{sha256.Sum256(data), bounds}
	}
	for _, link := range scene.Links {
		data, _ := json.Marshal(link)
		bounds := sourceLinkBounds(link, region)
		out.objects["link:"+link.ID] = sourceObject{sha256.Sum256(data), bounds}
	}
	return out
}

func sourceLinkBounds(link geom.Link, region geom.Bounds) geom.Bounds {
	// Link ownership and endpoint influence are column-based. Detour can
	// attach an endpoint outside the source Y range within climb tolerance;
	// every metadata change must therefore cover the region's full height.
	lower := func(value float64) float64 {
		if value < 0 && link.Radius > math.MaxFloat64+value {
			return -math.MaxFloat64
		}
		return value - link.Radius
	}
	upper := func(value float64) float64 {
		if value > 0 && link.Radius > math.MaxFloat64-value {
			return math.MaxFloat64
		}
		return value + link.Radius
	}
	return geom.Bounds{
		Min: geom.Vec3{X: lower(math.Min(link.Start.X, link.End.X)), Y: region.Min.Y, Z: lower(math.Min(link.Start.Z, link.End.Z))},
		Max: geom.Vec3{X: upper(math.Max(link.Start.X, link.End.X)), Y: region.Max.Y, Z: upper(math.Max(link.Start.Z, link.End.Z))},
	}
}

func sourceWorldBounds(b geom.Bounds, origin geom.Vec3) geom.Bounds {
	if origin == (geom.Vec3{}) {
		return b
	}
	// Outward rounding retains coverage when translating region-local bounds
	// back into world coordinates for source-border tile intersection tests.
	b.Min = geom.Vec3{X: math.Nextafter(b.Min.X+origin.X, math.Inf(-1)), Y: math.Nextafter(b.Min.Y+origin.Y, math.Inf(-1)), Z: math.Nextafter(b.Min.Z+origin.Z, math.Inf(-1))}
	b.Max = geom.Vec3{X: math.Nextafter(b.Max.X+origin.X, math.Inf(1)), Y: math.Nextafter(b.Max.Y+origin.Y, math.Inf(1)), Z: math.Nextafter(b.Max.Z+origin.Z, math.Inf(1))}
	return b
}

func changedSourceBounds(old, next sourceMetadata) []geom.Bounds {
	var boxes []geom.Bounds
	for id, before := range old.objects {
		after, exists := next.objects[id]
		if !exists || before.signature != after.signature {
			boxes = append(boxes, before.bounds)
			if exists {
				boxes = append(boxes, after.bounds)
			}
		}
	}
	for id, after := range next.objects {
		if _, exists := old.objects[id]; !exists {
			boxes = append(boxes, after.bounds)
		}
	}
	return boxes
}
