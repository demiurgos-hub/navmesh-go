# Ownership and world updates

`geom.Scene` retains independent terrain/assets/instances. Removing a building
removes its contribution, not the underlying terrain. Exclusion volumes remove
standing space; they are extended downward by agent height before clearance
erosion. Radius erosion applies afterward. Nonwalkable triangles remain solid
raster input. Area painting cannot restore an excluded span.

Use a separate dataset for each physical clearance/slope/climb profile unless
the caller explicitly accepts another profile's conservative restrictions.
Avoidance radius and traversal costs do not change baked clearance.

`bake.PrepareSource` validates and copies authored geometry into an opaque
immutable handle bound to the exact origin bits and strictness policy. It retains
canonical assets and placements instead of a triangle copy for every placement.
One validation/hash pass preserves exact resolved fingerprints and diagnostics.
The first nonempty build constructs shared local asset indexes and a placement
index; repeated `BuildTiles` calls expand only selected tile geometry. Concurrent builds can share the handle. Configuration, precision, resource
limits and output validation still run for each build. Result diagnostics and
link metadata, including nested profile lists, are independent copies; later
mutation of either the original scene or returned output cannot change the handle.

`dynamic.World` retains one prepared source for its current desired geometry;
an in-flight worker can retain an earlier coherent capture until it finishes.
`Apply` resolves accepted changes once, then publishes the source, metadata and
revision together. Failure or supersession leaves the prior capture intact.
This avoids validating and hashing the scene again during `Prepare`, at the
cost of retaining canonical assets, placements and shared local indexes alongside
authored source. Include those allocations and active build captures in memory
measurements. Prepared sources
retain no snapshots, and snapshots retain neither prepared sources nor indexes.

`dynamic.World.Apply` validates and copies desired state. Object revision zero
requests an automatic monotonic revision; an explicit stale revision is rejected.
Repeating the same object is idempotent. The union of old/new influence bounds
selects padded dirty tile columns, including vertical clearance for exclusions.
Repeated changes coalesce by tile identity. Dirty state is bounded by the fixed
region's maximum 65,536 columns. The library schedules no goroutines.

Large `Apply` calls belong on a host coordinator/worker: validation currently
resolves the scene to compute exact changed source bounds. That work occurs
outside the publication lock, and a concurrent desired edit can return
`ErrSuperseded` for recapture/retry. Coordinate actual geometry activation with
acceptance/quarantine; do not remove a physical bridge before the host marks its
navigation unavailable. `World.Pending()` is the constant-time tick readiness
check; the detailed `Status()` deliberately copies dirty diagnostics. `World.Safety()`
captures committed and desired revisions with a change token. It caches immutable
quarantine information per token and checks locations, paths, full segments and
traversal bounds against padded dirty columns across every vertical layer.

The host calls `Prepare` off-thread. It captures source/settings revisions,
rebakes complete dirty columns, shares unchanged immutable construction arrays,
and assembles an entirely new runtime mesh. Neighbor adjacency is never shared
between snapshots. `Commit` checks the captured desired/base revisions and
publishes the prepared pointer under a short lock. Failure, cancellation or
supersession preserves the old snapshot and pending desired state.

`Snapshot.RebuildColumns` accepts complete current metadata and caller-owned
replacement tiles for explicit whole columns. It copies replacement inputs,
removes every old layer in those columns, applies unloaded columns, and validates
the full merged bundle with the normal portable byte/tile/element limits before
runtime assembly. World, profile and settings must match the base. Shared
construction arrays retain no old snapshot or mutable runtime tile data;
`Snapshot.Bundle` still returns a fully independent deep copy. Keep the base
lease until rebuilding returns, and never expose construction arrays for mutation.
Direct callers supply trustworthy snapshot and tile revisions when geometry changes;
`dynamic.World` manages these identities for retained-source updates.

Readers acquire `World.Acquire()` leases and release them after their queries,
routes and handles are no longer needed. Each worker owns its own `Query` and
scratch; concurrent/reentrant use of one query is rejected. Filters are values
copied into query state. Queries invoke no gameplay callbacks. Snapshots are
immutable and can be read concurrently. Surface handles include snapshot
identity and cannot be persisted or silently used with another snapshot.

Query scratch grows lazily within configured limits. Full search pools are not
created by projection or ordinary local following. A host should discard the
job's search query after extracting owned results and create a separate query
for a following agent. Paths, corner/link copies, dependencies and `Move` visited
results never borrow scratch. `MoveWithoutVisited` omits only that owned output;
value and append accessors avoid temporary copies when converting paths.

`Snapshot.Topology()` retains only immutable dependency metadata and a small
private identity value, not its source snapshot or runtime mesh. Exact identity
allows checks against that same snapshot to skip redundant revision comparisons.
Equal world/revision numbers from another snapshot do not establish identity:
compatibility and dependency revisions still have to match. Caching topology or
safety metadata does not retain a managed generation or replace its lease.

Managed retention permits at most three generations including staging. If old
leases fill capacity, preparation returns `ErrBusy`; publication never waits for
readers. Hosts must cancel retired requests and release leases. Directly retaining
`World.Snapshot()` or a copied snapshot pointer bypasses retention accounting;
that convenience is for synchronous callers, not an unbounded worker queue.
Garbage collection provides memory safety, but it cannot enforce host budgets.

Geometry updates have desired state before committed navigation exists. The
host must quarantine affected routes/support while pending. Golem's regional
adapter pauses agents whose support or remaining corridor intersects quarantine;
unaffected agents continue. Failure or cancellation does not clear quarantine.
The runtime checks motor/traversal segments and collision readback too. Backends
without regional safety retain conservative profile-wide pauses. An old leased
snapshot remains queryable for completion/cancellation; it is not permission to
traverse removed world geometry.

Use bounded horizontal and separate up/down projection limits. A valid floor
hint preserves continuity; equally near stacked surfaces return ambiguity. A
partial path keeps its start-side prefix and has a separate limiting reason.
The endpoint is clamped to reachable space. Movement returns actual detail
height and a surface normal; it is not a swept collision motor. Raycasts test
walkability on the starting floor, not three-dimensional visibility.

`Query.RepairPath` remaps unchanged tile revisions, trims invalid suffixes and
joins small local movement. It requests replanning by returning stale when local
repair cannot establish continuity. The host increments destination generations,
rejects late results, handles braking/turning/stalls, and reconciles navigation
against actual body movement.

Rebasing transfers unchanged support by tile revision and polygon identity, never
by blindly selecting a nearby floor. Keep the old lease alive while creating the
latest view and transferring its location and route; release it only after the
whole transfer succeeds. Hosts must budget transfers fairly and skip intermediate
generations so paused agents cannot hold retired snapshots indefinitely. Use
`World.RetentionStats()` to observe managed leases and generations.

Choose the transfer operation according to the host's steering state.
`Query.RebaseUnchangedPath` requires the entire retained corridor to remain
unchanged and filter-eligible, including its suffix. It preserves the original
corners, endpoint and limiting status without repairing or trimming, so the host
keeps its corner cursor and any outstanding need to repair after lateral motion.
Changed dependencies return `Stale`. `Query.RebasePath` transfers exact support
and then performs corridor repair; a changed suffix can produce a partial route
whose endpoint is not the original destination. Neither operation selects another
support floor. A nil path transfers support only. Strict transfer returns a new
path bound to the target snapshot; it may share private immutable corner/tile
arrays, but does not retain the old snapshot. Public copied results remain owned
by their caller and independent of later queries.

Managed safety retains the union of changed columns since each still-leased old
generation. Unaffected old views can continue local movement outside that union
while Golem's separate rebase budget drains them. Local quarantine can therefore
outlast publication: `Safety.HasQuarantine()` may be true when desired and
committed revisions match. Releasing the last relevant old lease changes the
safety token; it never clears unrelated pending or unloaded columns. Changed
support must release its old view even if its agent is waiting in quarantine.

`Safety.ForRevision` selects a cached immutable scope for a managed generation.
It includes pending/unloaded columns and changes published since that generation,
so latest-view agents need not wait for unrelated old leases. Unknown revisions
return nil; each scope accepts locations and paths only from its own generation.
Scopes have distinct tokens and preserve full segment/bounds protection. The
global capture keeps the conservative union for callers without scoped selection.
These bounded cached scopes contain metadata, never runtime snapshots or leases;
the host must retain the selected lease and recapture after changes or callbacks.
Scoped handle checks require exact captured snapshot identity; matching artifact
metadata alone does not qualify. Repeated publication at one revision can contain
several current/leased identities, all bounded by managed retention. Releasing the
last old lease changes the capture token even when no quarantine columns were
added. Exact identity permits checking captured handles against the scope's full
change history without repeatedly comparing every dependency to the latest mesh.

`Snapshot.LinkStates()` reports attached endpoints and stable link revisions.
Detour attaches links inside the owner tile and immediate neighbors; distant
endpoints may remain unavailable. Hosts must inspect diagnostics and revalidate
availability/eligibility at begin and landing. Gameplay owns traversal actions,
interruption and cancellation. Disabling an optional link does not block an
ordinary doorway connection; that needs an exclusion or geometry edit.
Link metadata changes quarantine their endpoint/radius influence columns across
the entire region height. Endpoints can attach within climb tolerance outside
the authored source Y bounds, so their authored height cannot exclude a column
from invalidation.

The authoritative dynamic implementation always rebuilds from retained source.
The low-level cache is an explicit optimization with narrower geometry/height
semantics. Its safe loaders and failure handling do not establish blocker parity
for every profile. Do not automatically select it for production updates until
clearance, overlaps, height fidelity and capacity are qualified for that world.

For investigation, `debugger.Capture` creates copied geometry/link/route/update
data. `WriteJSON` records it; `WriteSVG` gives top and side views to reveal floor
selection. Bake diagnostics include source identity, stage errors, stage counts
and tile duration. Preserve failed input/configuration fingerprints alongside
host update/request generations when reporting a failure.
