# Third-party notices

## Initial source import

The initial navigation implementation was imported from
[o0olele/detour-go](https://github.com/o0olele/detour-go) at commit
[`ce0ee945eca49f2ac2040c2ec8706e3548d59a4d`](https://github.com/o0olele/detour-go/tree/ce0ee945eca49f2ac2040c2ec8706e3548d59a4d).
Its MIT license and copyright notice are preserved in [LICENSE](LICENSE).

The module path, internal imports, and project documentation have been adapted
for navmesh-go. This distribution is modified from the source identified above.

The imported Detour, tile-cache, and compression packages also record earlier
Go-port ancestry in
[fananchong/recastnavigation-go](https://github.com/fananchong/recastnavigation-go).

## Recast Navigation and Detour

The navigation code derives from
[Recast Navigation](https://github.com/recastnavigation/recastnavigation).
Mikko Mononen's copyright notices and the original Zlib license terms are
preserved in the applicable source-file headers.

## Recast Go translation

The initial `recast` package translation and its tests were imported from
[actfuns/recastnavigation](https://github.com/actfuns/recastnavigation) at commit
`9d1a09ada038de17331aa2933bda24f77d08c999`. That revision's README explicitly
declares the Zlib License, the same license as the original project. The complete
upstream license notice is retained in [recast/LICENSE](recast/LICENSE) and the
imported source headers. This code has been modified for navmesh-go, including
translation corrections, checked construction and explicit capacity failures.
No Detour implementation from that project is imported.

The C++ algorithm reference is Recast Navigation commit
`9f4ce64458dfae86e1239c525ddc219c4e9e06f1`. See
[the port manifest](recast/upstream-manifest.json) and
[provenance notes](recast/PROVENANCE.md) for correspondence, fixes, tests and
remaining qualification. The optional reference runner is a development tool;
production builds, baking and servers do not depend on it.

## FastLZ

The compression implementation retains Ariya Hidayat's 2005-2007 copyright
notices and MIT license in [fastlz/fastlz.go](fastlz/fastlz.go).

## Debug viewer

The bundled browser assets retain their embedded Three.js and Tweakpane
attribution and license notices.
