package artifact

import (
	"errors"
	"math"
	"sort"

	"github.com/demiurgos-hub/navmesh-go/detour"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

// cloneBundle preserves the portable round trip's validation, limits and
// canonical representation without materializing a serialized copy.
func cloneBundle(b Bundle) (Bundle, error) {
	if err := b.ValidatePortable(); err != nil {
		return Bundle{}, err
	}
	out := b
	out.Tiles = make([]Tile, len(b.Tiles))
	for i, tile := range b.Tiles {
		out.Tiles[i] = tile
		if tile.Empty {
			out.Tiles[i].Parameters = detour.DtNavMeshCreateParams{}
		} else {
			out.Tiles[i].Parameters = cloneParameters(tile.Parameters)
		}
		out.Tiles[i].Layers = make([]Layer, len(tile.Layers))
		for j, layer := range tile.Layers {
			out.Tiles[i].Layers[j] = layer
			out.Tiles[i].Layers[j].Heights = cloneValues(layer.Heights)
			out.Tiles[i].Layers[j].Areas = cloneValues(layer.Areas)
			out.Tiles[i].Layers[j].Connections = cloneValues(layer.Connections)
		}
	}
	sort.Slice(out.Tiles, func(i, j int) bool {
		a, b := out.Tiles[i].Key, out.Tiles[j].Key
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		return a.Layer < b.Layer
	})
	out.Links = make([]geom.Link, len(b.Links))
	for i, link := range b.Links {
		out.Links[i] = link
		out.Links[i].Profiles = cloneValues(link.Profiles)
		sort.Strings(out.Links[i].Profiles)
	}
	sort.Slice(out.Links, func(i, j int) bool { return out.Links[i].ID < out.Links[j].ID })
	return out, nil
}

func cloneValues[T any](values []T) []T {
	result := make([]T, len(values))
	copy(result, values)
	return result
}

func cloneParameters(p detour.DtNavMeshCreateParams) detour.DtNavMeshCreateParams {
	p.Verts = cloneValues(p.Verts)
	p.Polys = cloneValues(p.Polys)
	p.PolyFlags = cloneValues(p.PolyFlags)
	p.PolyAreas = cloneValues(p.PolyAreas)
	p.DetailMeshes = cloneValues(p.DetailMeshes)
	p.DetailVerts = cloneValues(p.DetailVerts)
	p.DetailTris = cloneValues(p.DetailTris)
	p.OffMeshConVerts = cloneValues(p.OffMeshConVerts)
	p.OffMeshConRad = cloneValues(p.OffMeshConRad)
	p.OffMeshConFlags = cloneValues(p.OffMeshConFlags)
	p.OffMeshConAreas = cloneValues(p.OffMeshConAreas)
	p.OffMeshConDir = cloneValues(p.OffMeshConDir)
	p.OffMeshConUserID = cloneValues(p.OffMeshConUserID)
	return p
}

type cloneBudget struct {
	bytes, elements uint64
	err             error
}

func (c *cloneBudget) add(n uint64) {
	if n > c.bytes {
		c.err = errors.New("artifact: invalid bundle size")
		return
	}
	c.bytes -= n
}
func (c *cloneBudget) str(s string) {
	if len(s) > 1<<20 {
		c.err = errors.New("artifact: string too long")
		return
	}
	c.add(4 + uint64(len(s)))
}
func (c *cloneBudget) count(n int, limit uint64) {
	if uint64(n) > math.MaxUint32 || uint64(n) > limit || uint64(n) > c.elements {
		c.err = errors.New("artifact: collection limit exceeded")
		return
	}
	c.elements -= uint64(n)
	c.add(4)
}
func (c *cloneBudget) sequence(n int, size uint64) {
	c.count(n, math.MaxUint32)
	c.add(uint64(n) * size)
}

func cloneLimits(b Bundle) error {
	return cloneLimitsWith(b, DefaultLimits())
}

func cloneLimitsWith(b Bundle, l Limits) error {
	c := cloneBudget{bytes: l.MaxBytes, elements: l.MaxElements}
	c.add(8 + 8 + 32)
	c.str(UpstreamRevision)
	c.str(ProducerVersion)
	c.str(b.WorldID)
	c.str(b.SourceFingerprint)
	c.str(b.ResolvedFingerprint)
	c.str(b.SettingsFingerprint)
	c.add(8)
	c.str(b.Profile.ID)
	c.add(32)
	c.add(129)
	c.str(b.Settings.Partition)
	c.count(len(b.Tiles), l.MaxTiles)
	for _, tile := range b.Tiles {
		c.add(21)
		if !tile.Empty {
			p := tile.Parameters
			c.add(85)
			c.sequence(len(p.Verts), 2)
			c.sequence(len(p.Polys), 2)
			c.sequence(len(p.PolyFlags), 2)
			c.sequence(len(p.PolyAreas), 1)
			c.sequence(len(p.DetailMeshes), 4)
			c.sequence(len(p.DetailVerts), 4)
			c.sequence(len(p.DetailTris), 1)
			c.sequence(len(p.OffMeshConVerts), 4)
			c.sequence(len(p.OffMeshConRad), 4)
			c.sequence(len(p.OffMeshConFlags), 2)
			c.sequence(len(p.OffMeshConAreas), 1)
			c.sequence(len(p.OffMeshConDir), 1)
			c.sequence(len(p.OffMeshConUserID), 4)
		}
		c.count(len(tile.Layers), 256)
		for _, layer := range tile.Layers {
			c.add(64)
			c.sequence(len(layer.Heights), 1)
			c.sequence(len(layer.Areas), 1)
			c.sequence(len(layer.Connections), 1)
		}
	}
	c.count(len(b.Links), l.MaxElements)
	for _, link := range b.Links {
		c.str(link.ID)
		c.add(69)
		c.str(link.Action)
		c.count(len(link.Profiles), 1024)
		for _, p := range link.Profiles {
			c.str(p)
		}
	}
	return c.err
}
