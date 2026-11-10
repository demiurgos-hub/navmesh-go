package recast

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// Partition selects the region partitioning algorithm.
type Partition uint8

const (
	// Monotone partitions quickly without overlapping regions.
	Monotone Partition = iota
	// Watershed uses a distance field to favor compact regions.
	Watershed
	// LayerRegions partitions into nonoverlapping regions suitable for tiles.
	LayerRegions
)

// AreaShape identifies a volume applied to the compact heightfield.
type AreaShape uint8

const (
	// BoxArea marks an axis-aligned box.
	BoxArea AreaShape = iota
	// ConvexArea marks a convex XZ polygon between Min[1] and Max[1].
	ConvexArea
	// CylinderArea marks an upright cylinder based at Min.
	CylinderArea
)

// AreaVolume paints existing walkable spans; it cannot introduce a surface.
// Volumes are applied in slice order before clearance erosion. Area zero
// excludes spans, and subsequent volumes cannot restore excluded spans.
type AreaVolume struct {
	Shape          AreaShape
	Min, Max       [3]float32
	Vertices       []float32
	Radius, Height float32
	Area           uint8
}

// BuildOptions controls optional layer output and per-build allocation limits.
// Zero limits use 4M cells, 8M spans and 1M detail sample candidates.
type BuildOptions struct {
	Partition          Partition
	BuildLayers        bool
	Volumes            []AreaVolume
	MaxCells, MaxSpans int
	// MaxDetailSamples bounds the sum of polygon detail sampling grid sizes.
	// It does not change requested sampling fidelity. Exceeding it fails the build.
	MaxDetailSamples int
}

// Diagnostic describes a nonfatal warning or failed algorithm stage.
type Diagnostic struct {
	Category LogCategory
	Message  string
}

// BuildStats records stage sizes without retaining intermediate fields.
type BuildStats struct{ RasterSpans, CompactSpans, MaxDistance, Regions, Contours int }

// BuildResult owns all generated slices. No slice aliases the caller's input.
// An empty Mesh is a successful empty tile, not a failed build.
type BuildResult struct {
	Stats       BuildStats
	Mesh        *PolyMesh
	Detail      *PolyMeshDetail
	Layers      *HeightfieldLayerSet
	Diagnostics []Diagnostic
}

var (
	// ErrInvalidInput indicates invalid geometry or bake settings.
	ErrInvalidInput = errors.New("recast: invalid input")
	// ErrCapacity indicates a documented representation or allocation limit.
	ErrCapacity = errors.New("recast: capacity exceeded")
	// ErrBuild indicates an algorithm failed to construct a valid mesh.
	ErrBuild = errors.New("recast: build failed")
)

// BuildError identifies the stage that rejected the candidate build.
type BuildError struct {
	Stage string
	Err   error
}

func (e *BuildError) Error() string { return "recast " + e.Stage + ": " + e.Err.Error() }
func (e *BuildError) Unwrap() error { return e.Err }

type diagnosticsKey struct{}
type diagnosticSink struct{ items []Diagnostic }

func report(ctx context.Context, category LogCategory, message string, args ...any) {
	if sink, ok := ctx.Value(diagnosticsKey{}).(*diagnosticSink); ok {
		if len(args) > 0 {
			message += ": " + fmt.Sprint(args...)
		}
		sink.items = append(sink.items, Diagnostic{category, message})
	}
}
func reportWarning(ctx context.Context, message string, args ...any) {
	report(ctx, LogWarning, message, args...)
}
func reportError(ctx context.Context, message string, args ...any) {
	report(ctx, LogError, message, args...)
}

// Build runs every Recast stage from resolved triangles to polygon/detail meshes.
// Geometry uses Y up and XZ horizontal coordinates. Config dimensions and bounds
// must already include tile padding. Triangles must have upward-facing winding.
// A nil areas slice gives every triangle WalkableArea before slope filtering;
// otherwise area zero is nonwalkable solid geometry. Caller inputs must remain
// unchanged for the duration of the call. Low-level stage APIs are intended for
// validated data; this entry point validates geometry, settings and output.
func Build(ctx context.Context, cfg Config, vertices []float32, triangles []int, areas []uint8, opt BuildOptions) (result *BuildResult, err error) {
	stage := "validation"
	defer func() {
		if p := recover(); p != nil {
			result = nil
			err = &BuildError{stage, fmt.Errorf("%w: internal invariant: %v", ErrBuild, p)}
		} else if err != nil {
			result = nil
			err = &BuildError{stage, err}
		}
	}()
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrInvalidInput)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = validateBuild(cfg, vertices, triangles, areas, opt); err != nil {
		return nil, err
	}
	sink := &diagnosticSink{}
	ctx = context.WithValue(ctx, diagnosticsKey{}, sink)
	detailBudget := newDetailSamplingBudget(opt.MaxDetailSamples)
	ctx = context.WithValue(ctx, detailSamplingKey{}, detailBudget)
	check := func(next string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		for _, d := range sink.items {
			if d.Category == LogError {
				return fmt.Errorf("%w: %s", ErrBuild, d.Message)
			}
		}
		stage = next
		return nil
	}
	result = &BuildResult{Mesh: &PolyMesh{Nvp: cfg.MaxVertsPerPoly, Bmin: cfg.Bmin, Bmax: cfg.Bmax, Cs: cfg.Cs, Ch: cfg.Ch}, Detail: &PolyMeshDetail{}}
	if len(triangles) == 0 {
		if opt.BuildLayers {
			result.Layers = &HeightfieldLayerSet{}
		}
		return result, nil
	}
	stage = "rasterization"
	hf := CreateHeightfield(ctx, cfg.Width, cfg.Height, cfg.Bmin, cfg.Bmax, cfg.Cs, cfg.Ch)
	hf.maxSpans = opt.MaxSpans
	if hf.maxSpans == 0 {
		hf.maxSpans = 8 << 20
	}
	triAreas := make([]uint8, len(triangles)/3)
	if areas == nil {
		for i := range triAreas {
			triAreas[i] = WalkableArea
		}
	} else {
		copy(triAreas, areas)
	}
	ClearUnwalkableTriangles(ctx, cfg.WalkableSlopeAngle, vertices, len(vertices)/3, triangles, len(triAreas), triAreas)
	for i := 0; i < len(triAreas); i += 128 {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		end := Min(i+128, len(triAreas))
		if err = RasterizeTriangles(ctx, vertices, len(vertices)/3, triangles[i*3:end*3], triAreas[i:end], end-i, hf, cfg.WalkableClimb); err != nil {
			if hf.capacityExceeded {
				return nil, ErrCapacity
			}
			return nil, err
		}
	}
	if err = check("filtering"); err != nil {
		return nil, err
	}
	if err = FilterLowHangingWalkableObstacles(ctx, cfg.WalkableClimb, hf); err != nil {
		return nil, err
	}
	if err = FilterLedgeSpans(ctx, cfg.WalkableHeight, cfg.WalkableClimb, hf); err != nil {
		return nil, err
	}
	if err = FilterWalkableLowHeightSpans(ctx, cfg.WalkableHeight, hf); err != nil {
		return nil, err
	}
	if err = check("compact heightfield"); err != nil {
		return nil, err
	}
	maxSpans := opt.MaxSpans
	if maxSpans == 0 {
		maxSpans = 8 << 20
	}
	if HeightFieldSpanCount(ctx, hf) > maxSpans {
		return nil, fmt.Errorf("%w: compact spans exceed %d", ErrCapacity, maxSpans)
	}
	result.Stats.RasterSpans = hf.activeSpans
	chf, e := BuildCompactHeightfield(ctx, cfg.WalkableHeight, cfg.WalkableClimb, hf)
	if e != nil {
		return nil, e
	}
	result.Stats.CompactSpans = chf.SpanCount
	if err = check("area marking and erosion"); err != nil {
		return nil, err
	}
	for _, v := range opt.Volumes {
		switch v.Shape {
		case BoxArea:
			err = MarkBoxArea(ctx, v.Min, v.Max, v.Area, chf)
		case ConvexArea:
			err = MarkConvexPolyArea(ctx, v.Vertices, len(v.Vertices)/3, v.Min[1], v.Max[1], v.Area, chf)
		case CylinderArea:
			err = MarkCylinderArea(ctx, v.Min, v.Radius, v.Height, v.Area, chf)
		}
		if err != nil {
			return nil, err
		}
	}
	if err = ErodeWalkableArea(ctx, cfg.WalkableRadius, chf); err != nil {
		return nil, err
	}
	if opt.BuildLayers {
		if err = check("heightfield layers"); err != nil {
			return nil, err
		}
		result.Layers = &HeightfieldLayerSet{}
		if err = BuildHeightfieldLayers(ctx, chf, cfg.BorderSize, cfg.WalkableHeight, result.Layers); err != nil {
			return nil, err
		}
	}
	if err = check("regions"); err != nil {
		return nil, err
	}
	switch opt.Partition {
	case Monotone:
		BuildRegionsMonotone(ctx, chf, cfg.BorderSize, cfg.MinRegionArea, cfg.MergeRegionArea)
	case Watershed:
		BuildDistanceField(ctx, chf)
		if !BuildRegions(ctx, chf, cfg.BorderSize, cfg.MinRegionArea, cfg.MergeRegionArea) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrBuild
		}
	case LayerRegions:
		if !BuildLayerRegions(ctx, chf, cfg.BorderSize, cfg.MinRegionArea) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrBuild
		}
	}
	result.Stats.MaxDistance = int(chf.MaxDistance)
	result.Stats.Regions = int(chf.MaxRegions)
	if err = check("contours"); err != nil {
		return nil, err
	}
	contours := BuildContours(ctx, chf, cfg.MaxSimplificationError, cfg.MaxEdgeLen, int(ContourTessWallEdges))
	if contours == nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrBuild
	}
	result.Stats.Contours = contours.Nconts
	if err = check("polygon mesh"); err != nil {
		return nil, err
	}
	if e = BuildPolyMeshChecked(contours, cfg.MaxVertsPerPoly, result.Mesh); e != nil {
		return nil, e
	}
	if err = check("detail mesh"); err != nil {
		return nil, err
	}
	if !BuildPolyMeshDetail(ctx, result.Mesh, chf, cfg.DetailSampleDist, cfg.DetailSampleMaxError, result.Detail) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if detailBudget.exceeded {
			return nil, fmt.Errorf("%w: detail sampling grid", ErrCapacity)
		}
		return nil, ErrBuild
	}
	if err = check("output validation"); err != nil {
		return nil, err
	}
	if e = ValidateMesh(result.Mesh, result.Detail); e != nil {
		return nil, e
	}
	result.Diagnostics = append([]Diagnostic(nil), sink.items...)
	return result, nil
}

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

func validateBuild(c Config, verts []float32, tris []int, areas []uint8, o BuildOptions) error {
	bad := func(s string) error { return fmt.Errorf("%w: %s", ErrInvalidInput, s) }
	maxCells := o.MaxCells
	if maxCells == 0 {
		maxCells = 4 << 20
	}
	if maxCells < 1 || o.MaxSpans < 0 || o.MaxDetailSamples < 0 {
		return bad("negative allocation limit")
	}
	if c.Width < 1 || c.Height < 1 || c.Width > maxCells/c.Height {
		return fmt.Errorf("%w: grid dimensions", ErrCapacity)
	}
	if c.Width >= 65535 || c.Height >= 65535 {
		return fmt.Errorf("%w: grid vertex range", ErrCapacity)
	}
	if !finite(c.Cs) || !finite(c.Ch) || c.Cs <= 0 || c.Ch <= 0 {
		return bad("cell size and height must be finite and positive")
	}
	for i := 0; i < 3; i++ {
		if !finite(c.Bmin[i]) || !finite(c.Bmax[i]) || c.Bmin[i] > c.Bmax[i] {
			return bad("invalid bounds")
		}
	}
	if float64(c.Bmax[1]-c.Bmin[1])/float64(c.Ch) >= SpanMaxHeight {
		return fmt.Errorf("%w: vertical voxel range", ErrCapacity)
	}
	if c.BorderSize < 0 || c.BorderSize >= c.Width/2 || c.BorderSize >= c.Height/2 {
		if c.BorderSize != 0 {
			return bad("border consumes tile")
		}
	}
	if c.WalkableHeight < 1 || c.WalkableHeight > 255 || c.WalkableClimb < 0 || c.WalkableClimb >= SpanMaxHeight || c.WalkableRadius < 0 || c.WalkableRadius > 127 {
		return bad("invalid clearance settings")
	}
	if !finite(c.WalkableSlopeAngle) || c.WalkableSlopeAngle < 0 || c.WalkableSlopeAngle >= 90 {
		return bad("slope must be in [0,90)")
	}
	if c.MaxVertsPerPoly < 3 || c.MaxVertsPerPoly > 6 {
		return bad("vertices per polygon must be 3..6")
	}
	if c.MinRegionArea < 0 || c.MergeRegionArea < 0 || c.MaxEdgeLen < 0 || !finite(c.MaxSimplificationError) || c.MaxSimplificationError < 0 || !finite(c.DetailSampleDist) || c.DetailSampleDist < 0 || !finite(c.DetailSampleMaxError) || c.DetailSampleMaxError < 0 {
		return bad("invalid mesh settings")
	}
	if c.MaxSimplificationError > maxDetailHeightSearchRadius {
		return fmt.Errorf("%w: simplification error exceeds detail height search range", ErrCapacity)
	}
	if o.Partition > LayerRegions {
		return bad("unknown partition mode")
	}
	if o.BuildLayers && (c.Width-2*c.BorderSize > 255 || c.Height-2*c.BorderSize > 255) {
		return fmt.Errorf("%w: layer dimensions exceed 255", ErrCapacity)
	}
	if len(verts)%3 != 0 || len(tris)%3 != 0 || (areas != nil && len(areas) != len(tris)/3) {
		return bad("malformed geometry slice lengths")
	}
	for _, v := range verts {
		if !finite(v) {
			return bad("nonfinite vertex")
		}
	}
	for _, i := range tris {
		if i < 0 || i >= len(verts)/3 {
			return bad("triangle index out of range")
		}
	}
	for _, a := range areas {
		if a > WalkableArea {
			return bad("area exceeds 63")
		}
	}
	for _, v := range o.Volumes {
		if v.Shape > CylinderArea || v.Area > WalkableArea {
			return bad("invalid area volume")
		}
		for i := 0; i < 3; i++ {
			if !finite(v.Min[i]) || !finite(v.Max[i]) {
				return bad("nonfinite volume")
			}
		}
		if v.Shape == BoxArea {
			for i := 0; i < 3; i++ {
				if v.Min[i] > v.Max[i] {
					return bad("inverted box")
				}
			}
		}
		if v.Shape == CylinderArea && (!finite(v.Radius) || !finite(v.Height) || v.Radius < 0 || v.Height < 0) {
			return bad("invalid cylinder")
		}
		if v.Shape == ConvexArea {
			if len(v.Vertices) < 9 || len(v.Vertices)%3 != 0 || v.Min[1] > v.Max[1] {
				return bad("invalid convex volume")
			}
			for _, p := range v.Vertices {
				if !finite(p) {
					return bad("nonfinite volume vertex")
				}
			}
		}
	}
	return nil
}

// ValidateMesh validates the connectivity and detail index ranges of a build.
func ValidateMesh(m *PolyMesh, d *PolyMeshDetail) error {
	if m == nil || d == nil || m.Nvp < 3 || m.Nvp > 6 || m.Nverts < 0 || m.Nverts >= 65535 || m.Npolys < 0 || m.Nverts > len(m.Verts)/3 || m.Npolys > len(m.Polys)/(2*m.Nvp) || m.Npolys > len(m.Areas) || m.Npolys > len(m.Flags) {
		return fmt.Errorf("%w: invalid polygon mesh", ErrBuild)
	}
	for i := 0; i < m.Npolys; i++ {
		n := 0
		p := m.Polys[i*2*m.Nvp : (i+1)*2*m.Nvp]
		for j := 0; j < m.Nvp && p[j] != MeshNullIdx; j++ {
			if int(p[j]) >= m.Nverts {
				return fmt.Errorf("%w: polygon vertex index", ErrBuild)
			}
			n++
			nei := p[m.Nvp+j]
			if nei != MeshNullIdx && nei&0x8000 == 0 && int(nei) >= m.Npolys {
				return fmt.Errorf("%w: polygon adjacency", ErrBuild)
			}
		}
		if n < 3 {
			return fmt.Errorf("%w: degenerate polygon", ErrBuild)
		}
	}
	if m.Npolys == 0 {
		return nil
	}
	if d.Nmeshes != m.Npolys || d.Nmeshes > len(d.Meshes)/4 || d.Nverts < 0 || d.Nverts > len(d.Verts)/3 || d.Ntris < 0 || d.Ntris > len(d.Tris)/4 {
		return fmt.Errorf("%w: invalid detail mesh", ErrBuild)
	}
	for i := 0; i < d.Nmeshes; i++ {
		p := d.Meshes[i*4 : i*4+4]
		vb, nv, tb, nt := uint64(p[0]), uint64(p[1]), uint64(p[2]), uint64(p[3])
		if nv < 3 || nt == 0 || vb+nv > uint64(d.Nverts) || tb+nt > uint64(d.Ntris) {
			return fmt.Errorf("%w: empty or invalid detail patch", ErrBuild)
		}
		for j := tb; j < tb+nt; j++ {
			for k := 0; k < 3; k++ {
				if uint64(d.Tris[j*4+uint64(k)]) >= nv {
					return fmt.Errorf("%w: detail triangle index", ErrBuild)
				}
			}
		}
	}
	for _, v := range d.Verts {
		if !finite(v) {
			return fmt.Errorf("%w: nonfinite detail vertex", ErrBuild)
		}
	}
	return nil
}
