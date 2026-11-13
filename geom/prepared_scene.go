package geom

import (
	"math"
	"sort"
)

// PreparedScene owns validated canonical assets and their placements without
// retaining a transformed triangle copy for every instance. It is immutable and
// safe to share. Use Scene.Prepare to construct it.
type PreparedScene struct {
	origin    Vec3
	assets    []Asset
	instances []preparedInstance
	metadata  Resolved
	count     int
}

type preparedInstance struct {
	object       Instance
	asset        int
	first, count int
	omitted      []int
	bounds       Bounds
}

// Prepare validates and copies the scene, preserving Resolve's exact ordering,
// diagnostics and fingerprints. Transformation and hashing run once; only shared
// source arrays, placement metadata and omission diagnostics are retained.
func (s Scene) Prepare(origin Vec3, strict bool) (*PreparedScene, error) {
	s = s.Clone()
	p := &PreparedScene{origin: origin, assets: s.Assets}
	assets := make(map[string]int, len(s.Assets))
	for i := range s.Assets {
		assets[s.Assets[i].ID] = i
	}
	objects := append([]Instance(nil), s.Instances...)
	sort.Slice(objects, func(i, j int) bool { return objects[i].ID < objects[j].ID })
	placements := make(map[string]int, len(objects))
	for _, obj := range objects {
		if obj.Disabled {
			continue
		}
		placements[obj.ID] = len(p.instances)
		p.instances = append(p.instances, preparedInstance{object: obj, asset: assets[obj.AssetID]})
	}
	digest := resolvedFingerprintStream{writer: newFingerprintWriter()}
	metadata, err := s.resolve(origin, strict, resolveOptions{retainMetadata: true, triangle: func(tr *ResolvedTriangle) {
		instance := &p.instances[placements[tr.ObjectID]]
		if instance.count == 0 {
			instance.bounds = tr.Bounds
		} else {
			instance.bounds = instance.bounds.Union(tr.Bounds)
		}
		instance.count++
		digest.triangle(tr)
	}})
	if err != nil {
		return nil, err
	}
	metadata.Fingerprint, err = s.Fingerprint()
	if err != nil {
		return nil, err
	}
	metadata.ResolvedFingerprint, err = digest.finish(metadata)
	if err != nil {
		return nil, err
	}
	for _, diagnostic := range metadata.Diagnostics {
		instance := &p.instances[placements[diagnostic.ObjectID]]
		if instance.count != 0 {
			instance.omitted = append(instance.omitted, diagnostic.Triangle)
		}
	}
	for i := range p.instances {
		p.instances[i].first = p.count
		p.count += p.instances[i].count
	}
	p.metadata = metadata
	return p, nil
}

// Metadata returns independently owned annotations, diagnostics and fingerprints.
// Triangles are accessed individually or through an index, never flattened here.
func (p *PreparedScene) Metadata() Resolved {
	if p == nil {
		return Resolved{}
	}
	r := p.metadata
	s := (Scene{Volumes: r.Volumes, Links: r.Links}).Clone()
	r.Volumes, r.Links = s.Volumes, s.Links
	r.Diagnostics = append([]Diagnostic(nil), r.Diagnostics...)
	return r
}

// TriangleCount returns the valid transformed triangle count without allocating.
func (p *PreparedScene) TriangleCount() int {
	if p == nil {
		return 0
	}
	return p.count
}

// TriangleAt transforms one triangle by value in Resolve's deterministic order.
// Invalid indices return false. Returned data never aliases the prepared scene.
func (p *PreparedScene) TriangleAt(index int) (ResolvedTriangle, bool) {
	if p == nil || index < 0 || index >= p.count {
		return ResolvedTriangle{}, false
	}
	i := sort.Search(len(p.instances), func(i int) bool { return p.instances[i].first+p.instances[i].count > index })
	instance := &p.instances[i]
	ordinal := index - instance.first
	if len(instance.omitted) != 0 {
		rank := ordinal
		ordinal = sort.Search(len(p.assets[instance.asset].Triangles), func(n int) bool {
			return n-sort.SearchInts(instance.omitted, n+1) >= rank
		})
	}
	tr, _, _ := resolveTriangle(p.assets[instance.asset], instance.object, p.origin, ordinal)
	return tr, true
}

// PreparedIndex shares one local spatial index per used canonical asset and a
// small index over placements. It owns no flattened transformed geometry and is
// safe for concurrent queries.
type PreparedIndex struct {
	source       *PreparedScene
	assets       []*Index
	placements   *Index
	placementIDs []int
}

// NewIndex constructs spatial acceleration for this immutable prepared scene.
// Retain and reuse the result across tile builds; construction is synchronous.
func (p *PreparedScene) NewIndex() *PreparedIndex {
	x := &PreparedIndex{source: p}
	if p == nil {
		return x
	}
	x.assets = make([]*Index, len(p.assets))
	var placements []Bounds
	for i, instance := range p.instances {
		if instance.count == 0 {
			continue
		}
		placements = append(placements, instance.bounds)
		x.placementIDs = append(x.placementIDs, i)
		if x.assets[instance.asset] != nil {
			continue
		}
		asset := p.assets[instance.asset]
		bounds := make([]Bounds, len(asset.Triangles))
		for j, tr := range asset.Triangles {
			box := Bounds{asset.Vertices[tr.Indices[0]], asset.Vertices[tr.Indices[0]]}
			for _, idx := range tr.Indices[1:] {
				v := asset.Vertices[idx]
				box = box.Union(Bounds{v, v})
			}
			bounds[j] = box
		}
		x.assets[instance.asset] = newBoundsIndex(bounds)
	}
	x.placements = newBoundsIndex(placements)
	return x
}

// Visit calls visit for exactly the triangles whose transformed bounds intersect
// box, in Resolve's source order. The first callback error stops the query.
func (x *PreparedIndex) Visit(box Bounds, visit func(ResolvedTriangle) error) error {
	if x == nil || x.source == nil {
		return nil
	}
	p := x.source
	for _, id := range x.placements.Query(box) {
		instance := &p.instances[x.placementIDs[id]]
		candidates := x.assets[instance.asset].query(func(local Bounds) bool {
			return transformedBounds(local, instance.object.Transform, p.origin).Intersects(box)
		})
		for _, ordinal := range candidates {
			tr, valid, _ := resolveTriangle(p.assets[instance.asset], instance.object, p.origin, ordinal)
			if valid && tr.Bounds.Intersects(box) {
				if err := visit(tr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Outward-rounded interval operations conservatively enclose Transform.Point,
// including cancellation, reflection, shear and subtraction of the region origin.
// Inverse-transforming a query box can lose boundary triangles for ill-conditioned
// affine transforms, so traversal transforms local node bounds instead.
func transformedBounds(b Bounds, t Transform, origin Vec3) Bounds {
	lo := [3]float64{b.Min.X, b.Min.Y, b.Min.Z}
	hi := [3]float64{b.Max.X, b.Max.Y, b.Max.Z}
	offset := [3]float64{origin.X, origin.Y, origin.Z}
	var min, max [3]float64
	for row := 0; row < 3; row++ {
		lower, upper := 0.0, 0.0
		for col := 0; col < 3; col++ {
			a, z := lo[col], hi[col]
			scale := t[row*4+col]
			if scale < 0 {
				a, z = z, a
			}
			a = math.Nextafter(scale*a, math.Inf(-1))
			z = math.Nextafter(scale*z, math.Inf(1))
			if col == 0 {
				lower, upper = a, z
			} else {
				lower = math.Nextafter(lower+a, math.Inf(-1))
				upper = math.Nextafter(upper+z, math.Inf(1))
			}
		}
		lower = math.Nextafter(lower+t[row*4+3], math.Inf(-1))
		upper = math.Nextafter(upper+t[row*4+3], math.Inf(1))
		lower = math.Nextafter(lower-offset[row], math.Inf(-1))
		upper = math.Nextafter(upper-offset[row], math.Inf(1))
		if math.IsNaN(lower) || math.IsNaN(upper) {
			lower, upper = math.Inf(-1), math.Inf(1)
		}
		min[row], max[row] = lower, upper
	}
	return Bounds{Vec3{min[0], min[1], min[2]}, Vec3{max[0], max[1], max[2]}}
}
