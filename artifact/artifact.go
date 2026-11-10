// Package artifact implements portable navigation bundles. The wire encoding is
// explicitly little-endian and independent of native Go/Detour memory layout.
package artifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// UpstreamRevision identifies the algorithm comparison baseline.
const UpstreamRevision = "9f4ce64458dfae86e1239c525ddc219c4e9e06f1"

// ProducerVersion identifies this unreleased artifact producer.
const ProducerVersion = "0.1.0-dev"
const magic = "NAVGO\x00\x01\x00"

// Settings records all resolved world-space bake settings. Region areas are in
// voxels, edge length in voxels, and sampling distances/errors in meters.
type Settings struct {
	Origin                                                             geom.Vec3
	Bounds                                                             geom.Bounds
	CellSize, CellHeight                                               float64
	TileSize                                                           int32
	Partition                                                          string
	MinRegionArea, MergeRegionArea, MaxEdgeLength                      int32
	MaxSimplificationError, DetailSampleDistance, DetailSampleMaxError float64
	BuildLayers                                                        bool
}

// TileKey identifies a tile layer within one world/profile bundle.
type TileKey struct{ X, Z, Layer int32 }

// Tile stores portable construction inputs. Empty is an explicit successful
// empty result, not an unloaded tile. Parameters are encoded field by field.
type Tile struct {
	Key        TileKey
	Revision   uint64
	Empty      bool
	Parameters detour.DtNavMeshCreateParams
	Layers     []Layer
}

// Layer stores uncompressed heightfield-layer arrays for a qualified cache path.
type Layer struct {
	BoundsMin, BoundsMax                              [3]float32
	CellSize, CellHeight                              float32
	Width, Height, MinX, MaxX, MinZ, MaxZ, HMin, HMax int32
	Heights, Areas, Connections                       []uint8
}

// Bundle describes one fixed navigation region and one baked physical profile.
type Bundle struct {
	WorldID, SourceFingerprint, ResolvedFingerprint, SettingsFingerprint string
	Revision                                                             uint64
	Profile                                                              geom.Profile
	Settings                                                             Settings
	Tiles                                                                []Tile
	Links                                                                []geom.Link
}

// Limits bounds untrusted decoding before allocation.
type Limits struct{ MaxBytes, MaxTiles, MaxElements uint64 }

// DefaultLimits returns conservative reader resource limits.
func DefaultLimits() Limits { return Limits{256 << 20, 65536, 16 << 20} }

type codec struct {
	r         *bytes.Reader
	w         *bytes.Buffer
	err       error
	remaining uint64
}

func (c *codec) value(v any) {
	if c.err != nil {
		return
	}
	if c.w != nil {
		c.err = binary.Write(c.w, binary.LittleEndian, v)
	} else {
		c.err = binary.Read(c.r, binary.LittleEndian, v)
	}
}
func (c *codec) str(s *string) {
	var n uint32
	if c.w != nil {
		if uint64(len(*s)) > 1<<20 {
			c.err = errors.New("artifact: string too long")
			return
		}
		n = uint32(len(*s))
	}
	c.value(&n)
	if c.err != nil {
		return
	}
	if c.w != nil {
		_, c.err = c.w.WriteString(*s)
	} else {
		if n > 1<<20 || uint64(n) > uint64(c.r.Len()) {
			c.err = errors.New("artifact: invalid string length")
			return
		}
		b := make([]byte, int(n))
		_, c.err = io.ReadFull(c.r, b)
		*s = string(b)
	}
}
func seq[T any](c *codec, s *[]T) {
	var n uint32
	if c.w != nil {
		if uint64(len(*s)) > math.MaxUint32 {
			c.err = errors.New("artifact: array too long")
			return
		}
		n = uint32(len(*s))
	}
	c.value(&n)
	if c.err != nil {
		return
	}
	if c.r != nil {
		var x T
		size := binary.Size(x)
		if size <= 0 || uint64(n) > c.remaining || uint64(n)*uint64(size) > uint64(c.r.Len()) {
			c.err = errors.New("artifact: invalid array length")
			return
		}
		c.remaining -= uint64(n)
		*s = make([]T, int(n))
	}
	c.value(s)
}
func (c *codec) boolean(b *bool) {
	var v uint8
	if *b {
		v = 1
	}
	c.value(&v)
	if v > 1 {
		c.err = errors.New("artifact: invalid boolean")
	}
	*b = v == 1
}
func (c *codec) profile(p *geom.Profile) {
	c.str(&p.ID)
	c.value(&p.Radius)
	c.value(&p.Height)
	c.value(&p.MaxSlope)
	c.value(&p.MaxClimb)
}
func (c *codec) settings(s *Settings) {
	c.value(&s.Origin)
	c.value(&s.Bounds)
	c.value(&s.CellSize)
	c.value(&s.CellHeight)
	c.value(&s.TileSize)
	c.str(&s.Partition)
	c.value(&s.MinRegionArea)
	c.value(&s.MergeRegionArea)
	c.value(&s.MaxEdgeLength)
	c.value(&s.MaxSimplificationError)
	c.value(&s.DetailSampleDistance)
	c.value(&s.DetailSampleMaxError)
	c.boolean(&s.BuildLayers)
}
func (c *codec) mesh(p *detour.DtNavMeshCreateParams) {
	seq(c, &p.Verts)
	c.value(&p.VertCount)
	seq(c, &p.Polys)
	seq(c, &p.PolyFlags)
	seq(c, &p.PolyAreas)
	c.value(&p.PolyCount)
	c.value(&p.Nvp)
	seq(c, &p.DetailMeshes)
	seq(c, &p.DetailVerts)
	c.value(&p.DetailVertsCount)
	seq(c, &p.DetailTris)
	c.value(&p.DetailTriCount)
	seq(c, &p.OffMeshConVerts)
	seq(c, &p.OffMeshConRad)
	seq(c, &p.OffMeshConFlags)
	seq(c, &p.OffMeshConAreas)
	seq(c, &p.OffMeshConDir)
	seq(c, &p.OffMeshConUserID)
	c.value(&p.OffMeshConCount)
	c.value(&p.UserId)
	c.value(&p.TileX)
	c.value(&p.TileY)
	c.value(&p.TileLayer)
	c.value(&p.Bmin)
	c.value(&p.Bmax)
	c.value(&p.WalkableHeight)
	c.value(&p.WalkableRadius)
	c.value(&p.WalkableClimb)
	c.value(&p.Cs)
	c.value(&p.Ch)
	c.boolean(&p.BuildBvTree)
}
func (c *codec) layer(l *Layer) {
	c.value(&l.BoundsMin)
	c.value(&l.BoundsMax)
	c.value(&l.CellSize)
	c.value(&l.CellHeight)
	c.value(&l.Width)
	c.value(&l.Height)
	c.value(&l.MinX)
	c.value(&l.MaxX)
	c.value(&l.MinZ)
	c.value(&l.MaxZ)
	c.value(&l.HMin)
	c.value(&l.HMax)
	seq(c, &l.Heights)
	seq(c, &l.Areas)
	seq(c, &l.Connections)
}
func (c *codec) count(n *uint32, limit, minimumBytes uint64) {
	c.value(n)
	if c.err == nil && (uint64(*n) > limit || (c.r != nil && (uint64(*n) > c.remaining || uint64(*n)*minimumBytes > uint64(c.r.Len())))) {
		c.err = errors.New("artifact: collection limit exceeded")
	}
	if c.err == nil && c.r != nil {
		c.remaining -= uint64(*n)
	}
}
func (c *codec) link(l *geom.Link) {
	c.str(&l.ID)
	c.value(&l.Revision)
	c.value(&l.Start)
	c.value(&l.End)
	c.value(&l.Radius)
	c.boolean(&l.Bidirectional)
	c.boolean(&l.Disabled)
	c.value(&l.Area)
	c.value(&l.Flags)
	c.str(&l.Action)
	n := uint32(len(l.Profiles))
	c.count(&n, 1024, 4)
	if c.r != nil && c.err == nil {
		l.Profiles = make([]string, int(n))
	}
	for i := range l.Profiles {
		c.str(&l.Profiles[i])
	}
}
func (c *codec) bundle(b *Bundle, limits Limits) {
	c.str(&b.WorldID)
	c.str(&b.SourceFingerprint)
	c.str(&b.ResolvedFingerprint)
	c.str(&b.SettingsFingerprint)
	c.value(&b.Revision)
	c.profile(&b.Profile)
	c.settings(&b.Settings)
	n := uint32(len(b.Tiles))
	c.count(&n, limits.MaxTiles, 25)
	if c.r != nil && c.err == nil {
		b.Tiles = make([]Tile, int(n))
	}
	for i := range b.Tiles {
		t := &b.Tiles[i]
		c.value(&t.Key)
		c.value(&t.Revision)
		c.boolean(&t.Empty)
		if !t.Empty {
			c.mesh(&t.Parameters)
		}
		nl := uint32(len(t.Layers))
		c.count(&nl, 256, 76)
		if c.r != nil && c.err == nil {
			t.Layers = make([]Layer, int(nl))
		}
		for j := range t.Layers {
			c.layer(&t.Layers[j])
		}
		if c.err != nil {
			return
		}
	}
	n = uint32(len(b.Links))
	c.count(&n, limits.MaxElements, 81)
	if c.r != nil && c.err == nil {
		b.Links = make([]geom.Link, int(n))
	}
	for i := range b.Links {
		c.link(&b.Links[i])
		if c.err != nil {
			return
		}
	}
}

// Encode writes canonical bytes, including an integrity checksum. It never
// serializes a native runtime tile or its mutable adjacency.
func Encode(b Bundle) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	if err := b.validateFingerprints(); err != nil {
		return nil, err
	}
	b.Tiles = append([]Tile(nil), b.Tiles...)
	sort.Slice(b.Tiles, func(i, j int) bool {
		a, z := b.Tiles[i].Key, b.Tiles[j].Key
		if a.X != z.X {
			return a.X < z.X
		}
		if a.Z != z.Z {
			return a.Z < z.Z
		}
		return a.Layer < z.Layer
	})
	b.Links = append([]geom.Link(nil), b.Links...)
	sort.Slice(b.Links, func(i, j int) bool { return b.Links[i].ID < b.Links[j].ID })
	for i := range b.Links {
		b.Links[i].Profiles = append([]string(nil), b.Links[i].Profiles...)
		sort.Strings(b.Links[i].Profiles)
	}
	c := codec{w: new(bytes.Buffer)}
	c.w.WriteString(magic)
	major, minor := uint16(1), uint16(0)
	features := uint32(0)
	c.value(&major)
	c.value(&minor)
	c.value(&features)
	upstream := UpstreamRevision
	c.str(&upstream)
	producer := ProducerVersion
	c.str(&producer)
	c.bundle(&b, DefaultLimits())
	if c.err != nil {
		return nil, c.err
	}
	sum := sha256.Sum256(c.w.Bytes())
	c.w.Write(sum[:])
	return c.w.Bytes(), nil
}

// Decode validates lengths and integrity before returning independently owned
// typed data. Unknown required features/versions fail before mesh construction.
func Decode(data []byte, limits Limits) (Bundle, error) {
	var b Bundle
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if uint64(len(data)) > limits.MaxBytes || len(data) < 8+8+32 {
		return b, errors.New("artifact: invalid bundle size")
	}
	body := data[:len(data)-32]
	sum := sha256.Sum256(body)
	if !bytes.Equal(sum[:], data[len(data)-32:]) {
		return b, errors.New("artifact: checksum mismatch")
	}
	if string(body[:8]) != magic {
		return b, errors.New("artifact: wrong magic")
	}
	c := codec{r: bytes.NewReader(body[8:]), remaining: limits.MaxElements}
	var major, minor uint16
	var features uint32
	c.value(&major)
	c.value(&minor)
	c.value(&features)
	if major != 1 || minor != 0 || features != 0 {
		return b, errors.New("artifact: unsupported version/features")
	}
	var upstream string
	c.str(&upstream)
	if upstream != UpstreamRevision {
		return b, errors.New("artifact: unsupported algorithm revision")
	}
	var producer string
	c.str(&producer)
	if producer == "" {
		return b, errors.New("artifact: missing producer version")
	}
	c.bundle(&b, limits)
	if c.err != nil {
		return Bundle{}, c.err
	}
	if c.r.Len() != 0 {
		return Bundle{}, errors.New("artifact: trailing data")
	}
	if err := b.Validate(); err != nil {
		return Bundle{}, err
	}
	if err := b.validateFingerprints(); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func (b Bundle) validateFingerprints() error {
	for _, s := range []string{b.SourceFingerprint, b.ResolvedFingerprint, b.SettingsFingerprint} {
		decoded, err := hex.DecodeString(s)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != s {
			return errors.New("artifact: fingerprint must be canonical SHA-256")
		}
	}
	return nil
}

// Clone copies through the canonical codec, including all geometry arrays.
func (b Bundle) Clone() (Bundle, error) {
	data, err := Encode(b)
	if err != nil {
		return Bundle{}, err
	}
	return Decode(data, DefaultLimits())
}

// Validate checks metadata and structural construction inputs without mutation.
func (b Bundle) Validate() error {
	if b.WorldID == "" {
		return errors.New("artifact: world ID required")
	}
	if err := b.Profile.Validate(); err != nil {
		return err
	}
	s := b.Settings
	if !s.Origin.Finite() || !s.Bounds.Valid() || !positive(s.CellSize) || !positive(s.CellHeight) || s.TileSize < 1 || s.TileSize > 4096 {
		return errors.New("artifact: invalid grid")
	}
	if s.Partition != "monotone" && s.Partition != "watershed" && s.Partition != "layers" {
		return errors.New("artifact: unsupported partition")
	}
	if s.MinRegionArea < 0 || s.MergeRegionArea < 0 || s.MaxEdgeLength < 0 || !nonnegative(s.MaxSimplificationError) || !nonnegative(s.DetailSampleDistance) || !nonnegative(s.DetailSampleMaxError) {
		return errors.New("artifact: invalid bake settings")
	}
	grid, err := validateGrid(s, b.Profile)
	if err != nil {
		return err
	}
	keys := map[TileKey]bool{}
	columns := map[[2]int32]int{}
	if _, err := (geom.Scene{Links: b.Links}).Resolve(geom.Vec3{}, false); err != nil {
		return err
	}
	linkByKey := map[uint32]geom.Link{}
	usedLinks := map[uint32]bool{}
	for _, l := range b.Links {
		linkByKey[geom.LinkKey(l.ID)] = l
	}
	for _, t := range b.Tiles {
		if keys[t.Key] || t.Key.Layer < 0 {
			return errors.New("artifact: duplicate/invalid tile key")
		}
		keys[t.Key] = true
		if float64(t.Key.X) < grid[0] || float64(t.Key.X) >= grid[1] || float64(t.Key.Z) < grid[2] || float64(t.Key.Z) >= grid[3] {
			return errors.New("artifact: tile outside region")
		}
		column := [2]int32{t.Key.X, t.Key.Z}
		columns[column]++
		if columns[column] > 32 {
			return errors.New("artifact: runtime tile column exceeds 32-layer query capacity")
		}
		if t.Empty {
			if t.Parameters.PolyCount != 0 {
				return errors.New("artifact: empty tile has polygons")
			}
		} else {
			p := t.Parameters
			if p.TileX != t.Key.X || p.TileY != t.Key.Z || p.TileLayer != t.Key.Layer {
				return errors.New("artifact: tile identity mismatch")
			}
			if !tileAligned(s, b.Profile, t.Key, p.Bmin, p.Bmax, false) {
				return fmt.Errorf("artifact: tile %v bounds %v..%v do not match grid", t.Key, p.Bmin, p.Bmax)
			}
			if err := validateMesh(p); err != nil {
				return fmt.Errorf("artifact: tile %v: %w", t.Key, err)
			}
			if p.Cs != float32(s.CellSize) || p.Ch != float32(s.CellHeight) || p.WalkableHeight != float32(b.Profile.Height) || p.WalkableRadius != float32(b.Profile.Radius) || p.WalkableClimb != float32(b.Profile.MaxClimb) {
				return errors.New("artifact: mixed tile/profile settings")
			}
			for i, key := range p.OffMeshConUserID {
				l, ok := linkByKey[key]
				if !ok || l.Disabled || l.Flags != p.OffMeshConFlags[i] || l.Area != p.OffMeshConAreas[i] || float32(l.Radius) != p.OffMeshConRad[i] {
					return errors.New("artifact: off-mesh link metadata mismatch")
				}
				eligible := len(l.Profiles) == 0
				for _, profile := range l.Profiles {
					eligible = eligible || profile == b.Profile.ID
				}
				tw := s.CellSize * float64(s.TileSize)
				if !eligible || usedLinks[key] || math.Floor((l.Start.X-s.Origin.X)/tw) != float64(t.Key.X) || math.Floor((l.Start.Z-s.Origin.Z)/tw) != float64(t.Key.Z) {
					return errors.New("artifact: duplicate, ineligible or wrongly owned off-mesh link")
				}
				usedLinks[key] = true
				expected := [6]float32{float32(l.Start.X - s.Origin.X), float32(l.Start.Y - s.Origin.Y), float32(l.Start.Z - s.Origin.Z), float32(l.End.X - s.Origin.X), float32(l.End.Y - s.Origin.Y), float32(l.End.Z - s.Origin.Z)}
				for j, v := range expected {
					if p.OffMeshConVerts[i*6+j] != v {
						return errors.New("artifact: off-mesh endpoint metadata mismatch")
					}
				}
				if (p.OffMeshConDir[i] != 0) != l.Bidirectional {
					return errors.New("artifact: off-mesh direction mismatch")
				}
			}
		}
		for _, l := range t.Layers {
			if !tileAligned(s, b.Profile, t.Key, l.BoundsMin, l.BoundsMax, true) {
				return errors.New("artifact: layer bounds do not match grid")
			}
			n := int64(l.Width) * int64(l.Height)
			if l.Width <= 0 || l.Height <= 0 || l.Width > 255 || l.Height > 255 || n != int64(len(l.Heights)) || n != int64(len(l.Areas)) || n != int64(len(l.Connections)) || l.HMax < l.HMin || l.HMax-l.HMin > 254 {
				return errors.New("artifact: invalid heightfield layer")
			}
			bounds := geom.Bounds{Min: geom.Vec3{X: float64(l.BoundsMin[0]), Y: float64(l.BoundsMin[1]), Z: float64(l.BoundsMin[2])}, Max: geom.Vec3{X: float64(l.BoundsMax[0]), Y: float64(l.BoundsMax[1]), Z: float64(l.BoundsMax[2])}}
			if !bounds.Valid() || l.CellSize != float32(s.CellSize) || l.CellHeight != float32(s.CellHeight) || l.MinX < 0 || l.MinZ < 0 || l.MaxX < l.MinX || l.MaxZ < l.MinZ || l.MaxX >= l.Width || l.MaxZ >= l.Height || l.HMin < 0 || l.HMax > 65535 {
				return errors.New("artifact: invalid heightfield layer bounds")
			}
			for _, area := range l.Areas {
				if area > 63 {
					return errors.New("artifact: invalid layer area")
				}
			}
		}
	}
	return nil
}
func positive(v float64) bool    { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func nonnegative(v float64) bool { return v == 0 || positive(v) }
func validateMesh(p detour.DtNavMeshCreateParams) error {
	if err := detour.ValidateCreateParams(&p); err != nil {
		return err
	}
	if p.VertCount < 3 || p.VertCount >= 65535 || p.PolyCount < 1 || p.PolyCount > 65535 || p.Nvp < 3 || p.Nvp > 6 || len(p.Verts) != int(p.VertCount)*3 || len(p.Polys) != int(p.PolyCount)*int(p.Nvp)*2 || len(p.PolyFlags) != int(p.PolyCount) || len(p.PolyAreas) != int(p.PolyCount) {
		return errors.New("invalid mesh counts")
	}
	if !positive(float64(p.Cs)) || !positive(float64(p.Ch)) || !positive(float64(p.WalkableHeight)) || !nonnegative(float64(p.WalkableRadius)) || !nonnegative(float64(p.WalkableClimb)) {
		return errors.New("invalid mesh dimensions")
	}
	for i := 0; i < 3; i++ {
		if math.IsNaN(float64(p.Bmin[i])) || math.IsInf(float64(p.Bmin[i]), 0) || math.IsNaN(float64(p.Bmax[i])) || math.IsInf(float64(p.Bmax[i]), 0) || p.Bmin[i] > p.Bmax[i] {
			return errors.New("invalid mesh bounds")
		}
	}
	for i := 0; i < int(p.PolyCount); i++ {
		count := 0
		ended := false
		for j := 0; j < int(p.Nvp); j++ {
			v := p.Polys[i*int(p.Nvp)*2+j]
			if v == 65535 {
				ended = true
				continue
			}
			if ended || v >= uint16(p.VertCount) {
				return errors.New("invalid polygon vertex")
			}
			count++
		}
		if count < 3 || p.PolyAreas[i] > 63 {
			return errors.New("invalid polygon")
		}
	}
	if p.DetailVertsCount < 0 || p.DetailTriCount < 0 || len(p.DetailVerts) != int(p.DetailVertsCount)*3 || len(p.DetailTris) != int(p.DetailTriCount)*4 || (len(p.DetailMeshes) != 0 && len(p.DetailMeshes) != int(p.PolyCount)*4) {
		return errors.New("invalid detail counts")
	}
	for _, v := range p.DetailVerts {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return errors.New("nonfinite detail vertex")
		}
	}
	n := int64(p.OffMeshConCount)
	if n < 0 || n > 65535 || int64(len(p.OffMeshConVerts)) != n*6 || int64(len(p.OffMeshConRad)) != n || int64(len(p.OffMeshConFlags)) != n || int64(len(p.OffMeshConAreas)) != n || int64(len(p.OffMeshConDir)) != n || int64(len(p.OffMeshConUserID)) != n {
		return errors.New("invalid off-mesh counts")
	}
	for _, v := range p.OffMeshConVerts {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return errors.New("nonfinite link endpoint")
		}
	}
	for i, r := range p.OffMeshConRad {
		if !positive(float64(r)) || p.OffMeshConAreas[i] > 63 || p.OffMeshConDir[i] > 1 {
			return errors.New("invalid link")
		}
	}
	return nil
}
