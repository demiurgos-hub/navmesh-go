// Package geom defines engine-independent, Y-up navigation source geometry.
// Coordinates are meters. Front faces use cross(b-a,c-a); upward faces have a
// positive Y normal. Inputs are copied when resolved or retained for updates.
package geom

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// Vec3 is a position or displacement in meters; X/Z form the horizontal plane.
type Vec3 struct{ X, Y, Z float64 }

// Bounds is an inclusive axis-aligned box.
type Bounds struct{ Min, Max Vec3 }

// Transform is a row-major affine matrix multiplying column vectors.
type Transform [16]float64

// Identity returns the identity transform.
func Identity() Transform { return Transform{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

// Translation returns a translation transform.
func Translation(p Vec3) Transform { t := Identity(); t[3] = p.X; t[7] = p.Y; t[11] = p.Z; return t }

// Point transforms a position.
func (t Transform) Point(p Vec3) Vec3 {
	return Vec3{t[0]*p.X + t[1]*p.Y + t[2]*p.Z + t[3], t[4]*p.X + t[5]*p.Y + t[6]*p.Z + t[7], t[8]*p.X + t[9]*p.Y + t[10]*p.Z + t[11]}
}

// Determinant returns the linear transform determinant.
func (t Transform) Determinant() float64 {
	return t[0]*(t[5]*t[10]-t[6]*t[9]) - t[1]*(t[4]*t[10]-t[6]*t[8]) + t[2]*(t[4]*t[9]-t[5]*t[8])
}

// Finite reports whether a position has finite coordinates.
func (p Vec3) Finite() bool { return finite(p.X) && finite(p.Y) && finite(p.Z) }
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Valid reports whether bounds are finite and ordered.
func (b Bounds) Valid() bool {
	return b.Min.Finite() && b.Max.Finite() && b.Min.X <= b.Max.X && b.Min.Y <= b.Max.Y && b.Min.Z <= b.Max.Z
}

// Intersects reports intersection including touching boundaries.
func (b Bounds) Intersects(c Bounds) bool {
	return b.Min.X <= c.Max.X && b.Max.X >= c.Min.X && b.Min.Y <= c.Max.Y && b.Max.Y >= c.Min.Y && b.Min.Z <= c.Max.Z && b.Max.Z >= c.Min.Z
}

// Expand expands all axes by the supplied displacement.
func (b Bounds) Expand(v Vec3) Bounds {
	return Bounds{Vec3{b.Min.X - v.X, b.Min.Y - v.Y, b.Min.Z - v.Z}, Vec3{b.Max.X + v.X, b.Max.Y + v.Y, b.Max.Z + v.Z}}
}

// Union returns bounds containing both boxes.
func (b Bounds) Union(c Bounds) Bounds {
	return Bounds{Vec3{math.Min(b.Min.X, c.Min.X), math.Min(b.Min.Y, c.Min.Y), math.Min(b.Min.Z, c.Min.Z)}, Vec3{math.Max(b.Max.X, c.Max.X), math.Max(b.Max.Y, c.Max.Y), math.Max(b.Max.Z, c.Max.Z)}}
}

// Triangle references three asset vertices. Area zero denotes a nonwalkable
// solid; areas 1..63 are candidates for slope classification.
type Triangle struct {
	Indices [3]uint32
	Area    uint8
}

// Asset is immutable indexed geometry with a stable source identity.
type Asset struct {
	ID        string
	Vertices  []Vec3
	Triangles []Triangle
}

// Instance places an asset. Transform must be explicit, including identity.
type Instance struct {
	ID, AssetID string
	Revision    uint64
	Transform   Transform
	Disabled    bool
}

// Volume paints or excludes standing space. Exclusions win over ordinary areas.
// Shape is box, cylinder, or convex; convex points are an X/Z polygon with Y
// bounds from Bounds. Priority then ID determines ordinary area paint order.
type Volume struct {
	ID       string
	Revision uint64
	Shape    string
	Bounds   Bounds
	Points   []Vec3
	Radius   float64
	Area     uint8
	Exclude  bool
	Priority int32
	Disabled bool
}

// Link describes authored traversal, independent of gameplay action execution.
type Link struct {
	ID                      string
	Revision                uint64
	Start, End              Vec3
	Radius                  float64
	Bidirectional, Disabled bool
	Area                    uint8
	Flags                   uint16
	Profiles                []string
	Action                  string
}

// LinkKey derives a stable compact runtime identifier. Scene validation rejects
// collisions; changing other links never renumbers an unchanged link.
func LinkKey(id string) uint32 {
	sum := sha256.Sum256([]byte(id))
	key := binary.LittleEndian.Uint32(sum[:4])
	if key == 0 {
		return 1
	}
	return key
}

// Scene retains independent assets, objects, annotations, and links.
type Scene struct {
	Assets    []Asset
	Instances []Instance
	Volumes   []Volume
	Links     []Link
}

// Profile specifies bake clearance and locomotion limits, not runtime avoidance.
// Baked boundaries approximate geometry; allow for grid and contour error and
// validate them against the actual body's collider dimensions.
type Profile struct {
	ID                                 string
	Radius, Height, MaxSlope, MaxClimb float64
}

// Diagnostic identifies an omitted or problematic source element.
type Diagnostic struct {
	ObjectID, Stage, Message string
	Triangle                 int
}

// ResolvedTriangle contains transformed, region-local vertices.
type ResolvedTriangle struct {
	Vertices       [3]Vec3
	Area           uint8
	ObjectID       string
	SourceTriangle int
	Bounds         Bounds
}

// Resolved is a deterministic source snapshot. Origin is subtracted exactly once.
type Resolved struct {
	Triangles           []ResolvedTriangle
	Volumes             []Volume
	Links               []Link
	Diagnostics         []Diagnostic
	Fingerprint         string
	ResolvedFingerprint string
}

// Validate checks an agent profile.
func (p Profile) Validate() error {
	if p.ID == "" || !finite(p.Radius) || p.Radius < 0 || !finite(p.Height) || p.Height <= 0 || !finite(p.MaxSlope) || p.MaxSlope < 0 || p.MaxSlope >= 90 || !finite(p.MaxClimb) || p.MaxClimb < 0 {
		return fmt.Errorf("geom: invalid profile %q", p.ID)
	}
	return nil
}

// Clone makes an independent deep copy of source data.
func (s Scene) Clone() Scene {
	out := s
	out.Assets = append([]Asset(nil), s.Assets...)
	for i := range out.Assets {
		out.Assets[i].Vertices = append([]Vec3(nil), s.Assets[i].Vertices...)
		out.Assets[i].Triangles = append([]Triangle(nil), s.Assets[i].Triangles...)
	}
	out.Instances = append([]Instance(nil), s.Instances...)
	out.Volumes = append([]Volume(nil), s.Volumes...)
	for i := range out.Volumes {
		out.Volumes[i].Points = append([]Vec3(nil), s.Volumes[i].Points...)
	}
	out.Links = append([]Link(nil), s.Links...)
	for i := range out.Links {
		out.Links[i].Profiles = append([]string(nil), s.Links[i].Profiles...)
	}
	return out
}

// Fingerprint returns a canonical SHA-256 of source content, independent of
// object/asset list order. Triangle order remains significant.
func (s Scene) Fingerprint() (string, error) {
	// Sorting owns only the containers; the immutable geometry arrays are read.
	s.Assets = append([]Asset(nil), s.Assets...)
	s.Instances = append([]Instance(nil), s.Instances...)
	s.Volumes = append([]Volume(nil), s.Volumes...)
	s.Links = append([]Link(nil), s.Links...)
	for i := range s.Assets {
		if len(s.Assets[i].Vertices) == 0 {
			s.Assets[i].Vertices = nil
		}
		if len(s.Assets[i].Triangles) == 0 {
			s.Assets[i].Triangles = nil
		}
	}
	for i := range s.Volumes {
		if len(s.Volumes[i].Points) == 0 {
			s.Volumes[i].Points = nil
		}
	}
	sort.Slice(s.Assets, func(i, j int) bool { return s.Assets[i].ID < s.Assets[j].ID })
	sort.Slice(s.Instances, func(i, j int) bool { return s.Instances[i].ID < s.Instances[j].ID })
	sort.Slice(s.Volumes, func(i, j int) bool { return s.Volumes[i].ID < s.Volumes[j].ID })
	sort.Slice(s.Links, func(i, j int) bool { return s.Links[i].ID < s.Links[j].ID })
	for i := range s.Links {
		s.Links[i].Profiles = append([]string(nil), s.Links[i].Profiles...)
		sort.Strings(s.Links[i].Profiles)
	}
	return fingerprintScene(s)
}

// Resolve validates and transforms all sources. Degenerate triangles are
// diagnosed and omitted, or rejected in strict mode. Nothing aliases the scene.
func (s Scene) Resolve(origin Vec3, strict bool) (Resolved, error) {
	return s.resolve(origin, strict, resolveOptions{retainTriangles: true, retainMetadata: true, fingerprints: true})
}

// Validate checks the same source and transformed geometry as Resolve without
// retaining resolved triangles, diagnostics or metadata, or computing hashes.
// Strict mode rejects degenerate triangles; otherwise they are omitted.
func (s Scene) Validate(origin Vec3, strict bool) error {
	_, err := s.resolve(origin, strict, resolveOptions{})
	return err
}

type resolveOptions struct {
	retainTriangles, retainMetadata, fingerprints bool
	// The pointer is borrowed only for the callback and reused for the next triangle.
	triangle func(*ResolvedTriangle)
}

func (s Scene) resolve(origin Vec3, strict bool, options resolveOptions) (Resolved, error) {
	var out Resolved
	if !origin.Finite() {
		return out, fmt.Errorf("geom: invalid origin")
	}
	s.Instances = append([]Instance(nil), s.Instances...)
	sort.Slice(s.Instances, func(i, j int) bool { return s.Instances[i].ID < s.Instances[j].ID })
	assets := map[string]Asset{}
	ids := map[string]bool{}
	for _, a := range s.Assets {
		if a.ID == "" || ids[a.ID] {
			return out, fmt.Errorf("geom: duplicate/empty asset %q", a.ID)
		}
		ids[a.ID] = true
		for _, v := range a.Vertices {
			if !v.Finite() {
				return out, fmt.Errorf("geom: asset %q has nonfinite vertex", a.ID)
			}
		}
		for _, tr := range a.Triangles {
			if tr.Area > 63 {
				return out, fmt.Errorf("geom: area exceeds 63")
			}
			for _, idx := range tr.Indices {
				if uint64(idx) >= uint64(len(a.Vertices)) {
					return out, fmt.Errorf("geom: asset %q triangle index outside vertices", a.ID)
				}
			}
		}
		assets[a.ID] = a
	}
	ids = map[string]bool{}
	triangleCapacity := 0
	for _, obj := range s.Instances {
		if obj.ID == "" || ids[obj.ID] {
			return out, fmt.Errorf("geom: duplicate/empty object %q", obj.ID)
		}
		ids[obj.ID] = true
		a, ok := assets[obj.AssetID]
		if !ok {
			return out, fmt.Errorf("geom: object %q missing asset %q", obj.ID, obj.AssetID)
		}
		t := obj.Transform
		for _, v := range t {
			if !finite(v) {
				return out, fmt.Errorf("geom: object %q nonfinite transform", obj.ID)
			}
		}
		det := t.Determinant()
		if !finite(det) || det == 0 || t[12] != 0 || t[13] != 0 || t[14] != 0 || t[15] != 1 {
			return out, fmt.Errorf("geom: object %q requires nonsingular affine transform", obj.ID)
		}
		if !obj.Disabled {
			if len(a.Triangles) > int(^uint(0)>>1)-triangleCapacity {
				return out, fmt.Errorf("geom: resolved triangle count overflow")
			}
			triangleCapacity += len(a.Triangles)
		}
	}
	if options.retainTriangles && triangleCapacity > 0 {
		out.Triangles = make([]ResolvedTriangle, 0, triangleCapacity)
	}
	var triangle ResolvedTriangle
	for _, obj := range s.Instances {
		if obj.Disabled {
			continue
		}
		a := assets[obj.AssetID]
		for n := range a.Triangles {
			var valid bool
			var err error
			triangle, valid, err = resolveTriangle(a, obj, origin, n)
			if err != nil {
				return out, err
			}
			if !valid {
				if options.retainMetadata {
					out.Diagnostics = append(out.Diagnostics, Diagnostic{obj.ID, "resolve", "degenerate triangle omitted", n})
				}
				if strict {
					return out, fmt.Errorf("geom: %s triangle %d is degenerate", obj.ID, n)
				}
				continue
			}
			if options.retainTriangles {
				out.Triangles = append(out.Triangles, triangle)
			}
			if options.triangle != nil {
				options.triangle(&triangle)
			}
		}
	}
	ids = map[string]bool{}
	for _, v := range s.Volumes {
		if err := validateVolume(v); err != nil {
			return out, err
		}
		if v.ID == "" || ids[v.ID] || !v.Bounds.Valid() || v.Area > 63 {
			return out, fmt.Errorf("geom: invalid volume %q", v.ID)
		}
		ids[v.ID] = true
		if v.Shape != "box" && v.Shape != "cylinder" && v.Shape != "convex" {
			return out, fmt.Errorf("geom: unsupported volume shape %q", v.Shape)
		}
		if v.Shape == "cylinder" && (!finite(v.Radius) || v.Radius <= 0) {
			return out, fmt.Errorf("geom: invalid cylinder radius")
		}
		// Radius participates in authored identity even for noncylinder volumes.
		if !finite(v.Radius) {
			return out, fmt.Errorf("geom: nonfinite volume radius %q", v.ID)
		}
		if v.Shape == "convex" && len(v.Points) < 3 {
			return out, fmt.Errorf("geom: convex volume needs three points")
		}
		if options.retainMetadata && !v.Disabled {
			v.Points = append([]Vec3(nil), v.Points...)
		}
		for i, p := range v.Points {
			if !p.Finite() {
				return out, fmt.Errorf("geom: invalid convex point")
			}
			point := sub(p, origin)
			if !v.Disabled && !point.Finite() {
				return out, fmt.Errorf("geom: volume %q point transform overflow", v.ID)
			}
			if options.retainMetadata && !v.Disabled {
				v.Points[i] = point
			}
		}
		v.Bounds.Min = sub(v.Bounds.Min, origin)
		v.Bounds.Max = sub(v.Bounds.Max, origin)
		if !v.Disabled && !v.Bounds.Valid() {
			return out, fmt.Errorf("geom: volume %q bounds transform overflow", v.ID)
		}
		if options.retainMetadata && !v.Disabled {
			out.Volumes = append(out.Volumes, v)
		}
	}
	sort.Slice(out.Volumes, func(i, j int) bool {
		a, b := out.Volumes[i], out.Volumes[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return a.ID < b.ID
	})
	ids = map[string]bool{}
	linkKeys := map[uint32]string{}
	for _, l := range s.Links {
		if l.ID == "" || ids[l.ID] || !l.Start.Finite() || !l.End.Finite() || !finite(l.Radius) || l.Radius <= 0 || l.Area > 63 || l.Flags == 0 {
			return out, fmt.Errorf("geom: invalid link %q", l.ID)
		}
		ids[l.ID] = true
		key := LinkKey(l.ID)
		if prior, ok := linkKeys[key]; ok {
			return out, fmt.Errorf("geom: runtime link key collision %q and %q", prior, l.ID)
		}
		linkKeys[key] = l.ID
		l.Start = sub(l.Start, origin)
		l.End = sub(l.End, origin)
		if !l.Disabled && (!l.Start.Finite() || !l.End.Finite()) {
			return out, fmt.Errorf("geom: link %q transform overflow", l.ID)
		}
		if options.retainMetadata && !l.Disabled {
			l.Profiles = append([]string(nil), l.Profiles...)
			sort.Strings(l.Profiles)
			out.Links = append(out.Links, l)
		}
	}
	sort.Slice(out.Links, func(i, j int) bool { return out.Links[i].ID < out.Links[j].ID })
	var err error
	if options.fingerprints {
		out.Fingerprint, err = s.Fingerprint()
		if err == nil {
			if len(out.Triangles) == 0 {
				out.Triangles = nil
			}
			out.ResolvedFingerprint, err = fingerprintResolved(out)
		}
	}
	return out, err
}

func resolveTriangle(asset Asset, obj Instance, origin Vec3, ordinal int) (ResolvedTriangle, bool, error) {
	tr := asset.Triangles[ordinal]
	r := ResolvedTriangle{Area: tr.Area, ObjectID: obj.ID, SourceTriangle: ordinal}
	for j, idx := range tr.Indices {
		v := obj.Transform.Point(asset.Vertices[idx])
		v.X -= origin.X
		v.Y -= origin.Y
		v.Z -= origin.Z
		if !v.Finite() {
			return r, false, fmt.Errorf("geom: transform overflow for %q", obj.ID)
		}
		r.Vertices[j] = v
	}
	if obj.Transform.Determinant() < 0 {
		r.Vertices[1], r.Vertices[2] = r.Vertices[2], r.Vertices[1]
	}
	u, v := sub(r.Vertices[1], r.Vertices[0]), sub(r.Vertices[2], r.Vertices[0])
	normal := Vec3{u.Y*v.Z - u.Z*v.Y, u.Z*v.X - u.X*v.Z, u.X*v.Y - u.Y*v.X}
	if !normal.Finite() {
		return r, false, fmt.Errorf("geom: object %q triangle %d exceeds numeric range", obj.ID, ordinal)
	}
	if normal.X == 0 && normal.Y == 0 && normal.Z == 0 {
		return r, false, nil
	}
	r.Bounds = Bounds{r.Vertices[0], r.Vertices[0]}
	for _, p := range r.Vertices[1:] {
		r.Bounds = r.Bounds.Union(Bounds{p, p})
	}
	return r, true, nil
}

func sub(a, b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

func validateVolume(v Volume) error {
	if !v.Bounds.Valid() {
		return fmt.Errorf("geom: invalid volume bounds %q", v.ID)
	}
	if v.Shape == "cylinder" {
		if !finite(v.Radius) || v.Radius <= 0 || v.Bounds.Max.X-v.Bounds.Min.X < 2*v.Radius || v.Bounds.Max.Z-v.Bounds.Min.Z < 2*v.Radius {
			return fmt.Errorf("geom: cylinder %q bounds must contain its radius", v.ID)
		}
	}
	if v.Shape != "convex" {
		return nil
	}
	if len(v.Points) < 3 || len(v.Points) > 256 {
		return fmt.Errorf("geom: convex volume %q needs 3..256 vertices", v.ID)
	}
	for _, p := range v.Points {
		if !p.Finite() || p.X < v.Bounds.Min.X || p.X > v.Bounds.Max.X || p.Z < v.Bounds.Min.Z || p.Z > v.Bounds.Max.Z {
			return fmt.Errorf("geom: convex volume %q point outside bounds", v.ID)
		}
	}
	sign := 0.0
	for i, a := range v.Points {
		b := v.Points[(i+1)%len(v.Points)]
		if a.X == b.X && a.Z == b.Z {
			return fmt.Errorf("geom: duplicate convex edge %q", v.ID)
		}
		for j, p := range v.Points {
			if j == i || j == (i+1)%len(v.Points) {
				continue
			}
			cross := (b.X-a.X)*(p.Z-a.Z) - (b.Z-a.Z)*(p.X-a.X)
			if !finite(cross) {
				return fmt.Errorf("geom: convex numeric overflow")
			}
			if cross == 0 {
				continue
			}
			s := math.Copysign(1, cross)
			if sign == 0 {
				sign = s
			} else if sign != s {
				return fmt.Errorf("geom: volume %q is not a simple convex polygon", v.ID)
			}
		}
	}
	if sign == 0 {
		return fmt.Errorf("geom: degenerate convex volume %q", v.ID)
	}
	return nil
}
