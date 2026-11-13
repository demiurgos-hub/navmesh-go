# Recast implementation provenance

The algorithm reference is Recast Navigation commit
`9f4ce64458dfae86e1239c525ddc219c4e9e06f1` (2026-02-27).
The starting Go translation and its tests were taken from
`actfuns/recastnavigation` commit `9d1a09ada038de17331aa2933bda24f77d08c999`,
package `recast` only. Its README explicitly declares “同原项目 — Zlib License”
(same as the original project — Zlib License). The complete upstream notice is
restored in LICENSE and the imported source headers. This is a modified
translation, not the original C++ implementation. No actfuns Detour code is used.

| Go file | Upstream reference |
| --- | --- |
| recast.go, types.go | Recast.cpp, Recast.h |
| rasterization.go | RecastRasterization.cpp |
| filter.go | RecastFilter.cpp |
| area.go | RecastArea.cpp |
| region.go | RecastRegion.cpp |
| contour.go | RecastContour.cpp and geometric helpers from RecastMeshDetail.cpp |
| mesh.go | RecastMesh.cpp |
| mesh_detail.go | RecastMeshDetail.cpp |
| layers.go | RecastLayers.cpp |

The imported tests correspond to Tests/Recast/Tests_Recast.cpp,
Tests_RecastFilter.cpp and Tests_RecastRasterization.cpp; integration tests were
also present in the Go source. Passing these tests is not proof of complete
upstream equivalence. The optional C++ oracle executes all three partition
modes against identical resolved fixtures and compares coverage, heights,
layers, and actual Detour projection/path/raycast results. Broader stage-level
comparisons remain a qualification requirement. Its explicit polygon-edge
height deviation is recorded in the manifest and oracle instructions.

The February 2026 invalid-span fix (`762ecb5d79ab5b5a936ecb9141f9844cf7bc04a6`)
and detail-mesh empty-triangle guard (`9f4ce64458dfae86e1239c525ddc219c4e9e06f1`)
are present. Local additions provide a validated Build entry point, structured
diagnostics, output checks, Go 1.20 compatibility, and explicit errors instead
of publishing results after detected capacity failures. Low-level stage APIs
retain structural correspondence and require validated intermediate objects.

Detail sampling has a cumulative default limit of 1,048,576 candidate grid
locations per `Build`, configurable with `BuildOptions.MaxDetailSamples`.
Polygon bounding rectangles are charged before sampling, including candidates
later rejected as outside a polygon. Coordinate conversion and four-int sample
storage are checked before allocation; exhaustion returns `ErrCapacity` without
changing the requested spacing. The low-level `BuildPolyMeshDetail` uses the
same default. Collection, refinement, and fallback height searches observe
context cancellation. `MaxSimplificationError` is limited to 23169 voxels so
the detail height-search spiral's integer iteration count remains representable
on 32-bit platforms; larger values fail validation rather than being clamped.

`DetailSampleDist == 0` retains and triangulates the original polygon hull,
without adding height samples. This intentionally repairs the pinned upstream
path that leaves its hull empty before triangulation. Heights then follow the
coarse polygon mesh, with the normal Recast detail-height offset; disabling
sampling does not promise approximation of source features absent from that
mesh. Positive spacing retains upstream edge limits and refinement behavior.

For an upstream update, review each relevant C++ diff against the pinned SHA,
record the mapped Go function and any intentional deviation, add a reproducer,
run both Go regressions and the optional C++ oracle, then update the pin. Do not
treat repository claims of synchronization as test evidence.

Wall contour simplification retains additional original raw samples when a
simplified chord introduces an intersection with a nonadjacent edge. The original
distance and maximum-edge passes run again after each refinement; no tolerance or
coordinate changes are applied. Shared region portal arcs remain unchanged, and
existing shared endpoints of weakly simple contours remain admissible. The exact
narrow-strip regression previously emitted a reversed triangle from a crossing
nine-vertex outline. This is an intentional correction beyond the pinned C++
distance-only simplifier, not a relaxation of Detour's polygon validation.

Contour triangulation validates the interior cone at both diagonal endpoints,
including its existing loose-collinearity path. Pinned C++ checks only the first
endpoint; duplicate bridge vertices around multiple holes invalidate the implied
second-endpoint admission. The exact six-hole regression retains all coordinates,
checks clockwise nonoverlapping triangles and exact outer-minus-hole area, and
keeps obstacle interiors excluded. This does not flip triangles, remove holes or
relax Detour validation.
