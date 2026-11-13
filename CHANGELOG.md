# Changelog

## Unreleased

- Narrow walkable contours retain raw wall samples when simplified chords cross
  or touch another edge. Refinement preserves the configured distance/edge limits
  and shared region portals instead of emitting invalid reversed polygons.

- Tile creation validation reports the invalid section and polygon/edge indices,
  preserving the same strict geometry and bounds checks.

- Added explicit binary32 clearance-ratio arithmetic for float-authored
  navigation settings. Height/radius ceil and climb floor share one conversion
  across validation, tile padding and baking. Binary64 remains the default with
  unchanged fingerprints and artifact bytes; binary32 artifacts use a required
  feature bit so older readers reject the new policy explicitly.

- Prepared navigation sources keep canonical assets and placement bounds rather
  than fully expanded per-instance triangles. Tile builds share local asset
  indexes and materialize only selected geometry, preserving exact fingerprints,
  rasterization order, transforms, diagnostics and independently owned results.
  Added `geom.Scene.Prepare` for reusable immutable instanced source queries.

- Added `geom.Scene.Validate` for full source/transform validation without
  retaining expanded triangles or computing discarded geometry fingerprints.
  It shares admission rules with `Resolve`, including strict degeneracy and
  enabled metadata overflow checks.

- Added coherent cached regional safety captures, conservative full-height dirty
  columns, unchanged-polygon support rebasing and managed retention diagnostics.
- Changed columns remain quarantined across leased retired generations, allowing
  hosts to budget rebases while distant old-view agents continue moving safely.
- Generation-scoped safety lets current views resume independently of unrelated
  retired leases. Captures and topology identities retain lightweight immutable
  metadata without extending runtime snapshot lifetimes.
  Scoped checks recognize exact current/leased snapshot identities, including
  separate no-op publications at one revision, and reject matching foreign data.
- Added strict whole-corridor transfer with `RebaseUnchangedPath`. Unchanged
  paths preserve corner order, endpoints and limiting status without repair;
  changed dependencies are rejected so hosts can select repair or replanning.
- Query scratch and search pools grow lazily within configured limits. Projection,
  movement without visited output and raycast reuse temporary storage; returned
  paths, corners, links and visited lists keep independent ownership. Strictly
  interior movement within one ground polygon needs no traversal scratch.
  Projection candidates grow on demand, and path extraction reserves at most
  the visited-node count within the configured corridor limit.
- Added `MoveWithoutVisited` and value/append path accessors for hosts that do not
  need visited-surface output or intermediate corner copies.
- Added deterministic terrain, multilevel and construction qualification worlds
  with ordinary-Git configuration fixtures and a standalone generator.
- Added a separate static capacity corridor with long, bidirectionally reachable
  routes for both clearance profiles. Multilevel floors use exterior ramps and
  overlapping landings, with baked connectivity regressions for every level.
- Corrected Recast border-vertex removal loops that skipped their first polygon
  edge and could index an empty vertex slot on repeated terrain features.
- Watershed regions now use the blurred distance field, matching the pinned C++
  implementation on the expanded terrain corpus.
- Projection ambiguity compares distances in consistent units, so adjacent
  terrain polygons do not incorrectly appear to be competing floors.
- Detail-height sampling uses stable triangle coordinates and a selected-polygon
  edge fallback, avoiding movement failures on thin terrain triangles. Edge
  rounding has a numeric margin capped by polygon size and agent radius; it
  does not change gameplay support tolerances or choose another floor.
- Artifact cloning and retained-source hashing preserve canonical bytes and
  ownership without whole-bundle serialization buffers. Search nodes grow in
  stable bounded blocks; node-pool memory accounting reports allocated storage.
- Column rebuilds share private immutable construction arrays for untouched
  tiles while assembling independent runtime meshes. Replacement inputs are
  copied and the complete merged bundle retains normal validation and limits.
- Added opaque `bake.PreparedSource` reuse for repeated builds. Dynamic worlds
  retain validated geometry and a lazy spatial index per desired source capture,
  avoiding repeated resolution while preserving independently owned outputs.
  This trades retained source/index memory for less repeated build work without
  retaining runtime snapshots.
- Checked path corners preserve authored off-mesh transitions even when their
  endpoints share horizontal coordinates, so hosts still run traversal policy.
- Link metadata edits invalidate all affected columns even when attached
  endpoints sit outside the authored source height range within climb tolerance.

- Added the complete pure-Go Recast stage pipeline, monotone/watershed/layer
  partitioning, detail meshes, heightfield layers and checked tile orchestration.
- Added generic transformed source geometry, profiles, portable versioned
  artifacts, a pure-Go baking command, checked runtime queries/corridor repair,
  immutable snapshots and retained-source dynamic rebuilds/checkpoints.
- Added stable traversal-link identities and attachment diagnostics, JSON/SVG
  debug captures and the headless source-to-restart example.
- Hardened malformed tile/layer loading, counts and buffer boundaries, FastLZ,
  crowd request handles, tile-cache retry/boundary behavior, polygon adjacency,
  raycast work limits and memory accounting. Invalid inputs now fail explicitly;
  callers relying on unchecked translated APIs must handle these failures.
- Added regressions, fuzz targets, a pinned optional C++ comparison executable,
  pure-Go dependency auditing and separate race/oracle CI lanes.
- Bounded detail sampling work, supported zero-distance base triangulation,
  rejected invalid tile-grid settings before allocation, and aligned checkpoint
  encoding limits with decoding. Streaming restoration preserves saved revisions.

This is an unreleased integration foundation. Production qualification and
published module versions are separate gates; see `docs/QUALIFICATION.md`.
