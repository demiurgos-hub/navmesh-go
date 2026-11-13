# Navigation artifacts, draft version 1

This document describes the implemented pre-release format. Original Detour
binary compatibility is not a requirement. The legacy `loader` package has its
own bounded importer; production bundles use `artifact`.

All integers have explicit widths and little-endian encoding. Floats are IEEE
binary32 or binary64, also little-endian. There is no alignment padding, pointer,
native `int`, or native struct overlay. A string is a uint32 byte length followed
by those bytes. An array is a uint32 count followed by field-wise encoded values.
A boolean is one byte, either 0 or 1. Go's `encoding/binary` processes struct
fields recursively; it does not copy Go's memory layout.

## Envelope

| Field | Encoding |
| --- | --- |
| Magic | 8 bytes: `NAVGO\x00\x01\x00` |
| Format major, minor | uint16 each; currently 1, 0 |
| Required features | uint32; bit 0 selects binary32 clearance ratios |
| Upstream algorithm SHA | string; exact supported pin |
| Producer version | string; currently `0.1.0-dev` |
| Bundle | fields below |
| Integrity checksum | SHA-256 of all preceding bytes, 32 bytes |

Unknown versions, required features, or algorithm pins fail. Version 1 currently
has no optional extension sections; unknown trailing bytes also fail. The
checksum detects corruption, not a malicious author. Every decoded mesh is
still validated before construction.

## Bundle fields, in wire order

1. World ID, source fingerprint, resolved-input fingerprint, settings fingerprint:
   four strings. Fingerprints are lowercase 64-character SHA-256 hex values.
2. Desired/committed revision: uint64.
3. Profile: ID string; radius, height, maximum slope in degrees, maximum climb:
   four float64 values.
4. Settings: origin XYZ and bounds min/max XYZ (float64); cell size and height
   (float64); tile size (int32); partition string (`monotone`, `watershed`,
   `layers`); minimum region area, merge area, maximum edge length (int32);
   simplification error, detail sample distance, detail error (float64); layer
   production boolean.
5. Tile array, sorted by signed X, Z, layer.
6. Authored link array, sorted by stable ID. Profile eligibility lists are sorted.

Coordinates are meters, Y up, X/Z horizontal. The fixed float64 region origin is
subtracted before local float32 baking. Tile coordinates use mathematical floor.
The voxel grid is shared by all tiles. Region/edge settings are in voxel units;
detail distances/errors are meters. Height and radius round up; climb rounds
down. Border padding is voxel radius plus three cells. Omitted/empty
`Settings.ClearancePrecision` preserves binary64 ratio arithmetic and the
original artifact bytes. Explicit `"binary32"` rounds profile/cell operands
and their quotient to binary32 before ceil/floor; required feature bit 0 records
this policy without changing the body layout. Older readers reject that bit.
Positive dimensions must remain positive and finite at the selected precision;
zero radius/climb remain allowed. No epsilon snaps values across boundaries.
The policy affects clearance counts and matching padding/validation only,
never source coordinates, transforms, bounds or terrain samples.

Source fingerprints hash canonical source assets, transforms, objects, volumes,
and links. Resolved fingerprints hash canonical transformed triangles, areas,
volumes, and links in region coordinates. Settings fingerprints include the
profile, grid, area-to-flag mapping and strict-input setting, excluding resource
budgets and transaction revision. They are provenance/cache keys, not evidence
that two arbitrary independently supplied tile payloads were baked correctly.

## Tiles and construction data

A tile is signed X/Z/layer (three int32), revision (uint64), empty boolean,
construction data when nonempty, then a heightfield-layer array. Empty means a
successful build with no navigable surface. A missing directory entry means
unloaded/unavailable. Neither is a failed bake silently converted to success.

Construction fields follow `artifact.codec.mesh` exactly:

- uint16 vertex array, int32 vertex count;
- uint16 polygon array (six or fewer vertices and adjacency), uint16 polygon
  flags, uint8 areas, int32 polygon count and vertices-per-polygon;
- uint32 detail descriptors, float32 detail vertices, int32 detail vertex count,
  uint8 detail triangles, int32 detail triangle count;
- float32 off-mesh endpoints/radii, uint16 flags, uint8 areas/directions, uint32
  stable user keys, int32 off-mesh count;
- uint32 user ID, int32 tile X/Z/layer;
- float32 bounds min/max, clearance height/radius/climb, cell size/height;
- BV-tree boolean.

Bounds must match the canonical tile grid. Recast's polygon-mesh upper Y bound
includes its voxelized height allowance; the raster region bounds remain fixed.
Internal polygon adjacency must be reciprocal over a reversed shared edge.
Self-links, degenerate/nonconvex ground polygons, invalid detail indexes and
unmatched link metadata are rejected.

The current baker writes one runtime tile per X/Z column containing all vertical
spans. Its independently produced heightfield layers are an auxiliary cache
representation, not separate floors selected by runtime layer ordinal. Imported
typed bundles are limited to 32 runtime layers per column to match the checked
query envelope. Layer ordinals are never persistent world identities.

A heightfield layer contains two float32 XYZ bounds, float32 cell dimensions,
eight int32 dimensions/ranges (width, height, min/max X, min/max Z, min/max height),
then byte arrays for heights, areas and connections. `bake.CompressLayers`
produces checked FastLZ data for the low-level tile cache. The artifact keeps
uncompressed typed layers; no implicit codec negotiation or native layout is used.

Links encode ID string, revision uint64, two float64 XYZ endpoints, radius
float64, two booleans (bidirectional, disabled), area uint8, flags uint16, action
string, and profile-name string array. Compact runtime keys derive from the
stable ID and are collision checked; they never depend on array order.

## Limits, publication and compatibility

Default decoding limits are 256 MiB encoded bytes, 65,536 tiles and 16 million
aggregate array/collection elements. Strings are limited to 1 MiB. Counts are
checked against remaining bytes and collection minimum sizes before allocation.
These are resource ceilings, not a recommended world size. Hosts may lower them.
Precision, compact-field, polygon, layer and reference-bit limits can reject a
smaller input. Validation also applies to explicitly empty datasets.

Canonical encoding of the same typed bundle with the same producer is identical.
Equivalent rebakes need not have identical polygon numbering. Upgrading format,
algorithm pin, grid, origin or physical profile requires an explicit migration
or rebake; do not silently combine incompatible datasets.

Write complete immutable generations to new paths and publish a host-owned
manifest last. The generic CLI writes one completed artifact through a sibling
temporary file; it does not implement a multi-file world checkpoint protocol.

## Sources and restart state

The development CLI consumes strict bounded JSON with `Scene`, `Config` and an
optional `Generator` ownership label. This is an authoring interchange document,
not the versioned runtime bundle. Scribe emits the same contract.

`dynamic.World.Checkpoint` is a separate versioned desired-state bundle:
`NGSTATE1` (8 bytes), SHA-256 (32 bytes), then strict JSON version 1. It includes
checkpoint ID, desired revision, source scene, settings, unloaded columns, and
source/settings fingerprints. Runtime polygon references are excluded. The host
coordinates entity state under the same checkpoint ID and commits its manifest
last. Restore validates every required part and rebuilds before readiness.
The complete encoded checkpoint, including its 40-byte envelope, is limited to
`dynamic.MaxCheckpointBytes` (256 MiB) by both writer and reader. Checkpoint IDs
must be nonempty valid UTF-8 and at most `dynamic.MaxCheckpointIDBytes` (4096 bytes).
Restoring unloaded columns preserves the saved desired revision; restoration
does not count as a new streaming edit. No intermediate loaded view is exposed.
