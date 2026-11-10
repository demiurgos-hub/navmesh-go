# Qualification record and remaining release gates

This implementation is an unreleased working-tree candidate, not a production
sign-off. The project has substantially more executable coverage than the initial
three helper tests, but neither a successful server demo nor a small differential
corpus proves all navigation geometry correct.

## Implemented paths

- Pure-Go geometry resolution, all Recast stages and partition modes, detail
  surfaces, heightfield layers, tiled builds, compressed-layer output and CLI.
- Checked portable artifacts, legacy imports, projection, ordinary/sliced path
  prefixes, surface movement, height queries, bounded raycasts and corridor repair.
- Retained source/object state, old/new influence tracking, revisioned private
  builds, atomic snapshot publication, cancellation/failure preservation, streaming
  and desired-state checkpoints.
- Separate Golem surface contracts/adapter, bounded independent queues, managed
  movement/body boundary, traversal lifecycle, readiness/checkpoints and a runnable
  server demonstration. Existing 2D backends remain separate.
- Explicit Scribe mesh/terrain/instance/area/exclusion/link authoring and source
  export. Collision footprints remain collision-only.
- Runtime/bake diagnostics, upstream mapping/provenance, optional actual C++
  comparisons, pure-Go CI, fuzz/race/checkptr lanes and dependency auditing.

The low-level cache has overlap/removal, malformed compression, empty-tile,
capacity and failure/retry regressions. Production dynamic updates deliberately
use full source rebuilding. Automatic cache selection and broad cache-vs-source
clearance/height parity are not implemented or claimed.

## Evidence captured during implementation

Windows development used Go 1.26.4, with ordinary `CGO_ENABLED=0`, `GOWORK=off`
tests. The full module also passed on the declared Go 1.20.14 minimum. Separate
Windows race runs used `CGO_ENABLED=1` and a C compiler; the instrumentation is
not an ordinary runtime dependency. Vet and checkptr are additional lanes, not
substitutes for geometry assertions.

The final 13-package library matrix passed on Windows/amd64 with Go 1.20.14 and
1.26.4, with separate race and checkptr runs on 1.26.4. Windows/386 binaries also
executed successfully on the development host. Linux/amd64 test executables were
cross-built by Go and actually executed in the installed Ubuntu/WSL environment,
from each package's correct working directory. Linux/arm64 was compilation-only.
Hosted CI and native Linux/arm64 execution remain release gates.

The optional oracle was built from pinned upstream
`9f4ce64458dfae86e1239c525ddc219c4e9e06f1`. It executes C++ Recast/Detour and Go on
identical inputs; the final 24-case suite passed across all partition modes with
93 query probes. Its checked-in protocol and fixtures cover stacked
surfaces/holes, slopes, padded tiles, clearance thresholds, wavy detail surfaces,
projection, paths and raycasts.
The manifest records deliberate polygon-edge height-query divergence. It is not
a claim of arbitrary C++ binary compatibility or exhaustive algorithm parity.

Decoder/compression/raster fuzzing received bounded local smoke runs, including
approximately 366,000 artifact inputs in one 11-second run. A weekly/manual
workflow runs eight bounded targets and preserves failures. The final checkpoint
decoder smoke run passed approximately 203,000 guided inputs in five seconds,
including checksum-resealed malformed bodies. These short observations
do not replace sustained fuzzing before a production release.

Scribe's portable tests and real Unity 6000.3.13f1 EditMode tests passed, including
the new navigation exporter. An actual C#-produced JSON fixture was consumed by
the Go baker. Unity 2021.3, the package's older supported baseline, was not installed
and is not qualified by the Unity 6 run.

Golem standalone checks use temporary candidate module archives and isolated
module caches because the new versions have not been published. Those checks
must not be represented as proof that the published versions exist. No local
workspace file or new local replacement directive is part of this change.

After Bitdefender interrupted temporary Windows test executables, local runs
resumed from fixed executable paths inside the user-excepted validation folder.
Four code-generation consumer checks were re-qualified with their temporary
Go/C# executables confined to that folder. The final run still excludes
`TestUnityRuntimePolicyConsumer`, whose .NET testhost location was not confirmed,
and reports the temporary checkout's existing optional dependency/privilege
skips. One unchanged WebTransport timing test failed
under load, then passed three isolated runs and a full package retry; retain the
failure as a timing-flake observation, not a navigation correction. The latest
server navigation, adapter, source builder and demonstration passed ordinary,
race and checkptr runs on Windows and native Linux/amd64 execution through WSL.
The final website build, server lifecycle demonstration and generated consumer
checks passed. These checks include bounded rebuild debounce/retry and physical
body offsets during surface steering and landing. They use refreshed candidate
archives rather than published module versions. Windows and Linux CLI bakes of
the actual Scribe fixture produced identical 14,255-byte artifacts.

## Performance observations

The checked-in benchmarks cover full baking, affected-tile rebuild/publication,
projection, paths and surface movement. Golem exposes queue/work/completion
latency histograms and maxima separately from the general task pool.

One development smoke run on an Intel Core Ultra 9 275HX, Windows/amd64, used the
16-column ground/bridge fixture. Across three 200ms benchmark samples it observed:

| Operation | Observed mean range | Allocated bytes/operation |
| --- | --- | --- |
| Projection | 4.1–4.8 microseconds | 16,672 |
| Path | 7.0–10.2 microseconds | 17,680 |
| Local move | 4.7–8.3 microseconds | 8,608 |
| Full fixture bake | 3.2–3.6 milliseconds | about 2.13 MiB |
| Affected rebuild plus private assembly | 2.5–2.7 milliseconds | about 1.13 MiB |

This run overlapped development/fuzz activity and preceded final hardening. It
is a smoke observation, not a reproducible release baseline, peak-memory estimate,
tail-latency result or agent-capacity claim. Rerun on an otherwise idle machine
against the exact release candidate and representative geometry.

## Required before production sign-off

1. Select the actual world, server hardware, tick rate, agent count, destination
   churn, geometry-update bursts and streaming workload. Measure p50/p95/p99,
   worst observed latency, peak RSS/heap, allocations, retained snapshots and
   tick interference. The plan's p99 tick <=2ms and query <=100ms targets remain
   provisional until that workload is approved and measured.
2. Expand the corpus beyond the current synthetic fixtures: representative stairs,
   spirals, detailed terrain, caves, dense multilayer buildings, profile thresholds,
   seams, construction/destruction and mixed traversal. Run sustained fuzzing and
   stateful restart/update/unload fault injection at deployment scale.
3. Run the hosted platform matrix, native Linux/arm64 and the Unity version(s)
   claimed for shipping. Add sanitizer runs for the optional C++ oracle where
   supported. Record exact source commits, toolchains and comparison input hashes.
4. Verify queue/snapshot memory bounds and long-duration fairness under overload.
   The initial Golem quarantine pauses a complete profile; tile-specific quarantine
   and snapshot geometry sharing are future optimizations, not correctness promises.
5. Confirm publication destination/visibility. Publish navmesh-go and the shared
   navigation module, then adapter/root consumers in dependency order; update
   real sums and rerun clean `GOWORK=off` consumers against published archives.
   Confirm fixtures arrive as real bytes, not LFS pointers.
6. Review the new APIs and format before tagging them. Align README, godoc, Golem
   rules/changelog, website docs and supported-platform claims with observed gates.

Optional local avoidance, automatic cache acceleration, broad legacy binary
compatibility, cross-region hierarchical routing and copy-on-write snapshot
optimization are not required to use the authoritative source-rebuild path.

Use `docs/OWNERSHIP.md` for concurrency/host responsibilities and `docs/FORMAT.md`
for artifact compatibility. The implementation makes broader Golem integration
possible; full production readiness requires the evidence above.
