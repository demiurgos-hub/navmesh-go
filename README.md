# navmesh-go

Pure-Go navigation mesh baking, surface queries and transactional world updates.
The module is `github.com/demiurgos-hub/navmesh-go`. It is an independent library:
it has no Golem Engine dependency and no third-party Go module dependencies.
Baking and deployed runtime operation do not use cgo, native bindings, shared
libraries or native subprocesses.

## Status

The source includes the complete Recast stage pipeline, all three region
partition modes, detail meshes, heightfield layers, portable artifacts, checked
runtime queries and retained-source tile rebuilding. This is an early API and
implementation, **not a production-readiness certification**. Regression tests
and a small real C++ differential corpus cover important cases; representative
game worlds, sustained workloads and broader algorithm qualification are still
required. A successful example does not establish correctness or capacity.

See the [artifact contract](docs/FORMAT.md), [ownership and update
contracts](docs/OWNERSHIP.md), and [qualification record](docs/QUALIFICATION.md).

## Packages and ownership

| Package | Responsibility |
| --- | --- |
| `navmesh` (module root) | Immutable snapshots; bounded projection, paths, movement, height and walkability raycasts |
| `geom` | Triangle assets, affine instances, area/exclusion volumes, authored links and physical agent profiles |
| `bake` | Profile/settings resolution; whole-region and affected-tile baking |
| `recast` | Rasterization, filtering, compact fields, erosion, regions, contours, polygon/detail meshes and layers |
| `artifact` | Versioned, bounded, checksummed, explicit little-endian construction-data encoding |
| `dynamic` | Source identities, desired revisions, dirty tiles, candidate preparation/publication, streaming and checkpoints |
| `detour` | Lower-level mesh storage, conversion and query algorithms |
| `crowd` | Lower-level corridors, crowd movement and avoidance |
| `tilecache`, `fastlz` | Layer compression and existing-ground obstacle updates; a separately qualified optimization |
| `loader` | Checked imports of the existing legacy mesh/cache sample formats |
| `debugger` | Debug drawing and the existing browser viewer |

Use the root package, `bake`, `geom`, `artifact` and `dynamic` for new integration.
Low-level translated APIs require callers to understand their intermediate-data
invariants and lifetimes. Golem owns its adapter, scheduling, entities, movement
and body coupling, and gameplay decisions.

## Run the server-side demonstration

From this checkout, with `CGO_ENABLED=0` set in your environment:

```sh
go run ./examples/headless
```

The example creates source geometry, bakes and round-trips an artifact, projects
onto a bridge, finds a route, steps on its surface, removes the bridge, rebuilds
and publishes a new snapshot, checks that ground remains, and restores desired
state from a checkpoint. It needs no editor or prebuilt navmesh.

The optional pure-Go command accepts a JSON document with `Scene` (`geom.Scene`)
and `Config` (`bake.Config`) fields, plus an optional `Generator` ownership label:

```sh
go run ./cmd/navmesh-bake -in scene.json -out world.nav
```

`bake.DefaultConfig` supplies demonstration values, not game requirements.
Set world bounds, origin, profile and resolution explicitly. The CLI rejects
unknown fields and writes a completed bundle through a temporary file.

## Library lifecycle

1. Construct `geom.Scene` with reusable assets and stable instance IDs. Resolve
   authoring transforms in float64. Coordinates are meters, **Y up**, horizontal
   X/Z; triangle normals use `cross(b-a, c-a)`. Reflections reverse winding.
   Zero transforms are invalid; use `geom.Identity()`.
2. Call `bake.Build(ctx, scene, config)` or `bake.BuildTiles` for selected columns.
   Radius/height round up to voxel clearance; climb rounds down. The default
   ratio arithmetic is binary64. Set `Settings.ClearancePrecision` to
   `artifact.ClearanceBinary32` for binary32-authored profile/cell dimensions;
   operands and quotient then round to binary32 before ceil/floor. This explicit
   policy never rounds geometry or changes the default arithmetic. Different
   physical profiles generally require separate baked datasets. Area costs and
   avoidance radius cannot replace baked clearance.
   For repeated builds from the same source, call `bake.PrepareSource` once and
   reuse its `BuildTiles` method with the same origin and strictness policy.
   The opaque handle retains canonical assets and placements, with one lazy
   local index per shared asset. Only geometry selected for a tile is expanded;
   fingerprints and rasterization order match the fully resolved source. Each
   build still validates its configuration and owns its outputs.
3. Persist with `artifact.Encode`; load with bounded `artifact.Decode` and
   `navmesh.NewSnapshot`. Artifacts store explicit construction data,
   fingerprints, settings and tile identities, not native pointer layouts.
   Arbitrary original-Detour binary compatibility is not promised.
4. Allocate `snapshot.NewQuery(navmesh.Limits{})` per owner. Use `Project` with
   separate horizontal/up/down limits, then `FindPath`, `Move`, `Height` and
   `Raycast`. Check partial/resource-limited outcomes. Keep surface handles to
   retain floor identity; use bounds and valid hints around stacked floors.
   `MoveWithoutVisited` omits the owned visited list when the host only needs
   final support. Scratch and search pools are lazy; give following agents a
   fresh local query instead of retaining a completed worker's search query.
5. For world changes, use `dynamic.World.Apply`, call `Prepare` on a host-owned
   worker, then `Commit` on the publication thread. Preparation rebuilds dirty
   columns and assembles an independent snapshot. Readers never observe tile
   mutation. Superseded candidates cannot commit. Retain managed snapshots with
   `Acquire`/`Release`; old handles remain tied to their originating snapshot.
   `World.Safety()` provides a coherent cached quarantine capture. Check support,
   corridor and full movement segments before using old geometry. Select
   `Safety.ForRevision` for the retained lease's generation when using scoped
   retirement protection. `Query.RebaseUnchangedPath` transfers exact support
   and a wholly unchanged corridor while preserving corners and steering cursor;
   `Query.RebasePath` can repair and trim a corridor instead. Release the old
   lease only after accepting the replacement handles together. See
   [ownership](docs/OWNERSHIP.md) for the distinction and lease requirements.
6. Persist `World.Checkpoint` under the host's coordinated checkpoint ID. Restore
   stable desired geometry and streaming intent, then rebuild before making
   agents ready. Runtime polygon references are not durable identities.

The library starts no worker pool and owns no engine tick. Hosts bound queues,
prioritize/cancel requests, invalidate affected routes, handle stalls and
disappearing surfaces, and reconcile body collision with navigation movement.
Ordinary moving characters use steering/avoidance/body movement, not per-frame
rebaking. Authored links describe traversal; the host executes gameplay actions.
Disabling an optional link does not close another route through a door.

Full source rebuilding supports added/removed walkable geometry and reveals
independently retained ground. Exclusion volumes can represent blockers. The
low-level tile cache only cuts existing baked layers; it cannot add a bridge or
recover omitted source surfaces. Its obstacle shapes need profile clearance,
and its height representation differs from full detail meshes. The dynamic
world therefore uses source rebuilding as its authoritative path.

## Limits and qualification

Use bounded local regions with a float64 world origin. Origins do not make
float32 Recast coordinates infinitely precise. Negative tile coordinates use
floor semantics. Heightfield spans, compact neighbor layers, region IDs,
polygon indices and compressed layers have finite representations.
`recast.Build` validates settings, enforces allocation limits and checks output;
low-level stage APIs expect validated intermediates.

Detail sampling defaults to at most 1,048,576 candidate grid locations per tile.
`bake.Config.MaxDetailSamplesPerTile` can set a different work budget; exceeding
it fails the candidate without silently reducing fidelity. Zero detail sample
distance explicitly disables additional sampling while retaining base polygon
triangulation. Invalid grid values and oversized checkpoint envelopes fail
before publication, and restoring unloaded columns preserves the saved revision.

Snapshot publication trades extra memory and private assembly time for reader
safety. Budget baking and query work independently; measure retained generations,
peak memory and tail latency on deployment workloads. There are no published
agent/world capacity guarantees.

## Development and validation

`go run ./cmd/navmesh-worldgen -config qualification/configs/terrain.json -out <new-directory>`
generates deterministic 256-metre terrain sources; `multilevel.json` and
`construction.json` select stacked structures and mixed dynamic scenes. Seeds,
two clearance profiles and input hashes are included. Keep generated worlds and
qualification copies under `C:/Demiurgos/navmesh-validation/world-test`. Golem's
separate `examples/navigation-stress` runner exercises the actual server against
these fixtures. Short smoke results do not establish the 1,000-agent acceptance
target; see the exact candidate's qualification record.

Go 1.20 is the minimum declared language version. CI specifies Go 1.20.14 and
Go 1.26.4, with Windows/amd64 and Linux/amd64/arm64 checks. These are configured
checks, not a claim that a hosted CI run has already passed.

```sh
go build ./...
go test ./...
go vet ./...
go test -gcflags=all=-d=checkptr=2 ./...
go run ./.github/scripts/check-purego
go test ./... -run '^$' -bench . -benchmem
```

Set `CGO_ENABLED=0` and `GOWORK=off` for ordinary checks. The separate race job
uses `CGO_ENABLED=1` and a C compiler for instrumentation; that is not a production
dependency. Run fuzz targets separately for sustained exploration. Benchmark
output is recorded without inventing capacity claims from synthetic scenes.
The server/baker dependency audit checks all non-test platform variants for
cgo, native binaries, common dynamic-library loaders, and subprocess imports.
It is a regression guard, not a proof against arbitrary hidden execution.
The scheduled/manual fuzz workflow bounds each of the eight fuzz targets to two
workers and two minutes and saves failure corpora for permanent regressions.

The optional C++ oracle is explicitly opted into and built separately against
revision `9f4ce64458dfae86e1239c525ddc219c4e9e06f1`. See
[oracle instructions](recast/testdata/oracle/README.md),
[port provenance](recast/PROVENANCE.md) and
[the upstream manifest](recast/upstream-manifest.json). Ordinary Go tests skip
the oracle. Its comparisons execute both implementations with identical resolved
inputs, including stacked surfaces and padded tiles. It compares surface
coverage/heights, layers, projections, ordinary and sliced path outcomes, and
raycasts. A documented polygon-edge height difference is checked explicitly;
the corpus does not establish complete upstream equivalence.

Required sample meshes and regression fixtures are real Git files so module
consumers receive usable data. Only optional screenshots use Git LFS. The
existing crowd example runs from `examples/crowd`; the viewer runs from
`examples/web` and serves `http://localhost:9001/public/`.

## License

See [LICENSE](LICENSE), [third-party notices](THIRD_PARTY_NOTICES.md),
[the Recast license](recast/LICENSE) and retained source-file notices.
