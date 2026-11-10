# Optional upstream comparison runner

This executable is development-only. Ordinary Go builds, tests, baking and
servers do not need it. `TestUpstreamOracle` skips unless
`NAVMESH_RECAST_ORACLE` explicitly names a compiled executable.

Build against an external checkout of Recast Navigation at exactly
`9f4ce64458dfae86e1239c525ddc219c4e9e06f1`. The input is a versioned whitespace
stream containing the same resolved float32 geometry/settings/volumes as the
Go build, followed by identical projection/path/raycast probes. Output is JSON
detail meshes, areas, layer counts, distance-field maximum and runtime outcomes.
The test compares sampled surface coverage, area IDs, stacked heights,
projection, height, complete and partial ordinary/sliced paths, and raycasts.
Coordinate/height/raycast tolerance is 0.0001 local world unit (ray fractions are
dimensionless); statuses and layer counts match exactly. Polygon numbering and
arbitrary bytes are not compared. These small fixtures are a starting
differential corpus, not full production qualification.

The corpus includes the exact resolved step geometry that exposed a Go
height-patch flood-fill queue bug (a consumed batch of exactly 256 entries was
not removed), both retained and fully eroded narrow passages, and a 13x13 wavy
terrain fixture requiring interior detail refinement at 0.75m sampling distance
and 0.03m detail error. All eight
scenes run under all three partition modes. In the 2m passage at 0.25m cells,
radius 0.5m retains a route while radius 0.75m leaves no usable polygon in both
implementations; physical clearance settings are conservatively quantized.

An explicit slope-edge probe records one intentional runtime deviation: at
projected position `(-4, 0.6, -4)`, Go accepts the polygon edge and returns height
`0.6` successfully; the pinned C++ `getPolyHeight` returns
`DT_FAILURE | DT_INVALID_PARAM` because its strict point-in-polygon check
excludes that edge. The test expects and reports this difference for all three
partition modes while still requiring matching projection, path and raycast
outcomes. Boundary height behavior remains subject to a broader corridor
contract review; ordinary interior probes require matching height status.

Example PowerShell, after placing the pinned checkout at `$referencePath`:

```powershell
if ((git -C $referencePath rev-parse HEAD) -ne '9f4ce64458dfae86e1239c525ddc219c4e9e06f1') { throw 'Unexpected upstream revision' }
$sourceFiles = (Get-ChildItem -LiteralPath "$referencePath/Recast/Source" -Filter '*.cpp').FullName
$sourceFiles += (Get-ChildItem -LiteralPath "$referencePath/Detour/Source" -Filter '*.cpp').FullName
$oraclePath = Join-Path $env:TEMP 'navmesh-recast-oracle.exe'
clang++ -std=c++11 -O2 -I "$referencePath/Recast/Include" -I "$referencePath/Detour/Include" recast/testdata/oracle/main.cpp @sourceFiles -o $oraclePath
$env:NAVMESH_RECAST_ORACLE = $oraclePath
$env:CGO_ENABLED = '0'
go test ./recast -run TestUpstreamOracle -v
```

Use equivalent compiler arguments on Linux. Do not enable fast-math. The
reference revision is emitted for traceability; the build step must also verify
the checkout. For native memory diagnostics build this runner separately with
the compiler's supported address/undefined-behavior sanitizers.
