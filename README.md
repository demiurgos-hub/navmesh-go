# navmesh-go

A Go library for navigation meshes, pathfinding, and movement queries.

The navigation runtime is implemented in Go and does not require cgo. The library
is independent of Golem Engine and can be used by other Go applications.

## Status

The current implementation supports queries over existing navigation meshes,
path corridors, crowd movement, and obstacle-driven tile updates. Building a
navigation mesh from arbitrary source geometry is not implemented yet.

The project is in early development. Loading, movement, and dynamic-update
behavior still require broader validation before production use.

## Packages

| Package | Purpose |
| --- | --- |
| `detour` | Navigation mesh storage, pathfinding, and surface queries |
| `crowd` | Path corridors, agent movement, and local avoidance |
| `tilecache` | Compressed navigation layers and obstacle-driven tile rebuilding |
| `loader` | Loading existing navigation mesh and tile-cache files |
| `fastlz` | Compression support for tile-cache data |
| `debugger` | Debug drawing and browser-viewer support |

The Go module path is `github.com/demiurgos-hub/navmesh-go`.

## Development

Run from the repository root:

```sh
go test ./...
go vet ./...
```

Set `CGO_ENABLED=0` to explicitly verify builds without cgo. This module has no
third-party Go module dependencies.

## Examples

The `examples/crowd` program loads the included sample mesh. Run it from its own
directory so its relative asset path resolves correctly:

```sh
cd examples/crowd
go run .
```

The browser debugger can be started from `examples/web` with `go run .`; its
viewer is served at `http://localhost:9001/public/`.

The small sample navigation mesh is stored in regular Git so it remains usable
in Go module downloads. Optional screenshots under `examples/assets` use Git LFS.

## License

See [LICENSE](LICENSE), [third-party notices](THIRD_PARTY_NOTICES.md), and the
license headers retained in individual source files.
