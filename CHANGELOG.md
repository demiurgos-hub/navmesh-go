# Changelog

## Unreleased

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
