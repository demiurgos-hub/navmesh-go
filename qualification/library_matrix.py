"""Build and execute one recorded library qualification lane from a staged copy.

Linux lanes cross-compile with the supplied Windows Go toolchain and execute the
resulting binaries in Ubuntu/WSL. They are explicitly not native Linux compiler
qualification. Every generated file stays below the validation root.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


ROOT = Path("C:/Demiurgos/navmesh-validation/world-test").resolve()


def linux_path(path):
    value = str(Path(path).resolve()).replace("\\", "/")
    return "/mnt/" + value[0].lower() + value[2:]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--go", required=True, type=Path)
    parser.add_argument("--target", choices=("windows", "linux"), default="windows")
    parser.add_argument("--mode", choices=("pure", "checkptr", "race"), default="pure")
    parser.add_argument("--cc")
    args = parser.parse_args()
    workspace, output = args.workspace.resolve(), args.output.resolve()
    for path in (workspace, output):
        if not path.is_relative_to(ROOT):
            parser.error("workspace and output must be staged below " + str(ROOT))
    if args.target == "linux" and args.mode == "race":
        parser.error("this runner does not provide a Linux cross C compiler")
    output.mkdir(parents=True, exist_ok=False)
    bin_dir = ROOT / "bin" / output.name / (args.target + "-amd64")
    temp_dir = ROOT / "scratch" / output.name
    cache_dir = ROOT / "cache" / output.name
    for path in (bin_dir, temp_dir, cache_dir / "build", cache_dir / "modules", output / "logs"):
        path.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    settings = {
        "GOWORK": "off", "CGO_ENABLED": "1" if args.mode == "race" else "0",
        "GOFLAGS": "-mod=readonly", "GOTOOLCHAIN": "local", "GOENV": "off", "GOTELEMETRY": "off",
        "GOOS": args.target, "GOARCH": "amd64",
        "TEMP": str(temp_dir), "TMP": str(temp_dir),
        "TMPDIR": str(temp_dir), "GOTMPDIR": str(temp_dir),
        "GOCACHE": str(cache_dir / "build"), "GOMODCACHE": str(cache_dir / "modules"),
    }
    if args.cc:
        settings["CC"] = args.cc
    env.update(settings)
    env.pop("NAVMESH_RECAST_ORACLE", None)
    record = {"workspace": str(workspace), "output": str(output), "target": args.target,
              "sources": [{"path": p.relative_to(workspace).as_posix(), "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
                          for p in sorted(workspace.rglob("*")) if p.is_file()],
              "mode": args.mode, "environment": settings, "commands": [], "executables": [],
              "compiler_host": "windows", "execution_host": "Ubuntu/WSL" if args.target == "linux" else "windows",
              "go": str(args.go), "go_sha256": hashlib.sha256(args.go.read_bytes()).hexdigest(),
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "exclusions": ["Optional C++ oracle is qualified separately; no oracle executable supplied to this lane."]}
    if args.mode != "pure":
        record["exclusions"].append("Named Allocations tests are skipped under diagnostic instrumentation; ordinary lanes enforce allocation counts.")

    def save():
        (output / "results.json").write_text(json.dumps(record, indent=2) + "\n")

    def run(command, cwd=workspace, timeout=600):
        index = len(record["commands"])
        log = output / "logs" / (str(index).zfill(3) + ".txt")
        started = time.time()
        with log.open("w", encoding="utf-8") as stream:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
        record["commands"].append({"argv": [str(x) for x in command], "cwd": str(cwd),
                                   "exit": result.returncode, "seconds": time.time() - started, "log": str(log)})
        save()
        if result.returncode:
            print(log.read_text(encoding="utf-8"))
            raise SystemExit(result.returncode)
        return log.read_text(encoding="utf-8")

    run([str(args.go), "version"])
    package_lines = run([str(args.go), "list", "-f", "{{.ImportPath}}|{{.Dir}}|{{len .TestGoFiles}}|{{len .XTestGoFiles}}", "./..."])
    run([str(args.go), "vet", "./..."])
    flags = ["-race"] if args.mode == "race" else (["-gcflags=all=-d=checkptr=2"] if args.mode == "checkptr" else [])
    count = 0
    for line in package_lines.splitlines():
        package, directory, tests, external = line.split("|")
        if int(tests) + int(external) == 0:
            continue
        name = Path(directory).relative_to(workspace).as_posix().replace("/", "_")
        if name == ".":
            name = "root"
        executable = bin_dir / (name + (".test.exe" if args.target == "windows" else ".test"))
        run([str(args.go), "test", "-c", "-vet=off", *flags, "-o", str(executable), package])
        record["executables"].append({"package": package, "path": str(executable),
                                       "sha256": hashlib.sha256(executable.read_bytes()).hexdigest(), "bytes": executable.stat().st_size})
        if args.target == "windows":
            command = [str(executable), "-test.v", "-test.count=1"]
        else:
            linux_env = {key: linux_path(value) if key in ("TEMP", "TMP", "TMPDIR", "GOTMPDIR", "GOCACHE", "GOMODCACHE") else value for key, value in settings.items()}
            command = ["wsl", "-d", "Ubuntu", "--cd", linux_path(directory), "--exec", "env",
                       *[key + "=" + value for key, value in linux_env.items()],
                       linux_path(executable), "-test.v", "-test.count=1"]
        if args.mode != "pure":
            command += ["-test.skip=/.*Allocations$"]
        run(command, cwd=directory)
        count += 1
        print(args.target, args.mode, package, "PASS", flush=True)
    record["tested_packages"] = count
    record["complete"] = True
    save()
    print(output / "results.json", flush=True)


if __name__ == "__main__":
    main()
