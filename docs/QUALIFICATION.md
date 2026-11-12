# Navigation qualification

This is an unpublished working-tree candidate. The requested local 1,000-agent
qualification is complete with measured limits; separate release gates remain.
No tag, remote or publication is implied by a local module archive. The
[initial integration record](qualification/initial-integration-record.md) preserves
earlier observations; its profile-wide pauses and provisional targets describe
that earlier implementation, not this candidate.

## Current behavior

Production runtime and baking use `CGO_ENABLED=0`. Regional safety quarantines
changed tile columns across all floors and bake/clearance influence. Failure and
cancellation retain quarantine. Golem keeps unaffected agents moving and transfers
unchanged support/corridors to the latest view, preserving floor identity and intent.
Missing profiles and restoration remain global barriers. Other backends can retain
conservative profile-wide pauses.

Following queries own bounded local scratch separately from job-owned search memory.
Returned paths, corners, links, dependencies and visited lists remain independent
of later queries. Ordinary tests enforce warm scratch allocation counts; race and
checkptr instrumentation qualify correctness separately.

## Workload and evidence

Reusable generation lives in `qualification/worldgen` and `cmd/navmesh-worldgen`.
The actual Server runner, staging and release tools live in Golem Engine's
`examples/navigation-stress` and `qualification`. Canonical configurations and
reduced regressions are ordinary Git files. All generated execution material lives
in `C:/Demiurgos/navmesh-validation/world-test`, or the corresponding `/mnt/c/...`
path under Ubuntu/WSL: `worlds`, `workspaces`, `bin`, `scratch`, `cache` and `runs`.

The requested workload scope is 1,000 active agents at 20 Hz only. On 2026-11-11,
the user excluded future 100-agent and 5,000-agent tiers from this pass. They are
excluded work, not failed or pending qualification; historical evidence is preserved.
Seeded 256-metre terrain, multilevel and construction families use positive and
negative coordinates and two clearance profiles. Defaults are 10% destination
replacement per second, two geometry edits per second and periodic 20-edit bursts.
On each platform, run the three family cases at 1,000 agents with two minutes
of warm-up and ten measured minutes each. Then run one two-hour
construction/streaming soak at the same population. The separate static Capacity
case also uses 1,000 agents. Short rehearsals do not qualify these durations.
Both static Capacity runs, all three mixed cases and both two-hour soaks have
completed on Windows/amd64 and native Ubuntu/WSL Linux/amd64. Golem Engine's
`qualification/REPORT.md` records their exact candidates and outcomes. The
static cases kept all 1,000 actors moving on each measured tick; mixed soaks had
many quarantined or stationary actors and do not establish that same capacity.

Navigation p99 ≤5 ms remains the recorded performance target. The user waived
meeting it as a completion blocker; measured values and target failures remain in
the evidence. Both static Capacity runs and the three Windows mixed cases
missed this target; the three Linux mixed cases and both soaks met it within
their measured activity mixes. Whole-tick overruns and tick-count deficits remain
reported separately; passing navigation timing does not prove perfect 20 Hz
wall-clock cadence. Correctness, successful request-to-admission p99 ≤100 ms outside
observed quarantine, unaffected-agent cadence, bounded generations, drained memory
returning to the recorded steady-state envelope, platform checks, and the full
workload and soak durations remain required. Failed, cancelled, superseded and
unresolved requests are recorded separately. Budgets cannot improve reported timing
by starving agents. Timing runs exclude simultaneous fuzzing and benchmarks.

Golem's Windows budgets and interval observations use `NavigationNow`, backed by
the high-resolution performance counter. Pair timestamps from that clock with
`Time.Sub`; do not mix them with `time.Since`, `time.Until` or ordinary `time.Now`
readings. Civil timestamps and runtime GC wall-time correlation are recorded
separately; that correlation is approximate. Earlier Windows rehearsals using
coarse ordinary clock readings remain historical evidence, not directly
comparable measurements or proof of the required gates. Preserve the exact
clock implementation and candidate manifest with each timing result.

Support tolerance is an explicit fixture setting aligned with bake error and below
distinct-floor separation. The Golem default remains 0.05 metres. A reconstructed
terrain edit moved baked support down 0.056493 m at the same XZ with 0.1 m allowed
detail error, so strict default rejection was correct. Qualification uses cell height
plus detail error (0.2 m), with a separate removed-floor no-auto-snap regression.
This does not change the tick, path, cadence or ownership gates.

## Reproducible checks

1. Stage exact sources using Golem Engine's `qualification/stage.py`. Use `GOWORK=off`
   and keep candidate checksums/overrides in validation copies.
2. Run ordinary tests, vet and checkptr for the library, shared nav, adapter, engine
   and both navigation consumers. Check library Go 1.20.14, shared nav/adapter
   Go 1.25.0, engine/consumers Go 1.25.4 and current Go 1.26.4. Race instrumentation
   is a separate lane with its required C compiler.
3. Run the standalone C++ oracle pinned to
   `9f4ce64458dfae86e1239c525ddc219c4e9e06f1`. The expanded corpus has 39 cases
   and 150 runtime probes; it is not exhaustive parity or arbitrary binary compatibility.
4. Fuzz every listed target for ten minutes with bounded workers and preserve crashes.
   Run baseline/candidate microbenchmarks in a separate quiet window.
5. Execute Windows/amd64 and native Ubuntu/WSL Linux/amd64 workloads and available
   Unity 6 authoring/export/bake checks. Retain hardware, toolchains, exact commands,
   source/input/executable hashes, settings, worker counts, results and exclusions.
6. Index evidence and update Golem Engine's final qualification report. Failed
   rehearsals remain evidence; passing unit tests do not replace workload gates.

## Separate release gates

Publication remains a later action: navmesh-go `v0.1.0` and shared nav
`golem/nav/v0.4.0`, adapter `golem/nav/navmesh/v0.1.0`, engine `v0.4.0`, then clean
consumers with genuine published checksums. See Golem Engine's
`qualification/RELEASE.md`. Private archives do not prove published resolution.

Native ARM64, Unity 2021.3 and hosted execution remain pending. Linux server
navigation checks are separate from the root repository's existing Ebiten graphics
dependency, which requires cgo there. Earlier unrelated networking test failures
remain visible as historical records; the final documented server-package
matrices passed at their attributed source identities.

Automatic cache selection, cross-request search pooling, a mutable public corridor
API, safe-prefix movement toward quarantine and cross-region hierarchical routing
remain outside this pass. See [ownership](OWNERSHIP.md) and [format](FORMAT.md).
