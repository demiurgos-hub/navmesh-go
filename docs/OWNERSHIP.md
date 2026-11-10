# Ownership and world updates

`geom.Scene` retains independent terrain/assets/instances. Removing a building
removes its contribution, not the underlying terrain. Exclusion volumes remove
standing space; they are extended downward by agent height before clearance
erosion. Radius erosion applies afterward. Nonwalkable triangles remain solid
raster input. Area painting cannot restore an excluded span.

Use a separate dataset for each physical clearance/slope/climb profile unless
the caller explicitly accepts another profile's conservative restrictions.
Avoidance radius and traversal costs do not change baked clearance.

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
check; the detailed `Status()` deliberately copies dirty diagnostics.

The host calls `Prepare` off-thread. It captures source/settings revisions,
rebakes complete dirty columns, copies unchanged construction payloads, and
assembles an entirely new runtime mesh. Neighbor adjacency is never shared
between snapshots. `Commit` checks the captured desired/base revisions and
publishes the prepared pointer under a short lock. Failure, cancellation or
supersession preserves the old snapshot and pending desired state.

Readers acquire `World.Acquire()` leases and release them after their queries,
routes and handles are no longer needed. Each worker owns its own `Query` and
scratch; concurrent/reentrant use of one query is rejected. Filters are values
copied into query state. Queries invoke no gameplay callbacks. Snapshots are
immutable and can be read concurrently. Surface handles include snapshot
identity and cannot be persisted or silently used with another snapshot.

Managed retention permits at most three generations including staging. If old
leases fill capacity, preparation returns `ErrBusy`; publication never waits for
readers. Hosts must cancel retired requests and release leases. Directly retaining
`World.Snapshot()` or a copied snapshot pointer bypasses retention accounting;
that convenience is for synchronous callers, not an unbounded worker queue.
Garbage collection provides memory safety, but it cannot enforce host budgets.

Geometry updates have desired state before committed navigation exists. The
host must quarantine affected routes/support while pending. Golem's initial
adapter conservatively pauses the entire affected profile, including failed
pending updates, until a replacement commits. This is safe but can stop more
agents than a future tile-specific quarantine. An old leased snapshot remains
queryable for completion/cancellation; it is not permission to traverse removed
world geometry.

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

`Snapshot.LinkStates()` reports attached endpoints and stable link revisions.
Detour attaches links inside the owner tile and immediate neighbors; distant
endpoints may remain unavailable. Hosts must inspect diagnostics and revalidate
availability/eligibility at begin and landing. Gameplay owns traversal actions,
interruption and cancellation. Disabling an optional link does not block an
ordinary doorway connection; that needs an exclusion or geometry edit.

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
