"""Prepare coverage-instrumented binaries, then fuzz existing targets.

The default runs all eight targets sequentially for ten minutes with two workers.
Separate prepare/run phases let an external coordinator launch target-specific
processes as an explicitly recorded cohort, without overlapping compilation.
Schedule every fuzz run outside performance measurements.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time


TARGETS = [
    ("artifact", "FuzzDecode"), ("detour", "FuzzDecodeNavMeshData"),
    ("loader", "FuzzDecodeLegacyMeshSets"), ("fastlz", "FuzzDecompress"),
    ("tilecache", "FuzzDecodeLayer"), ("recast", "FuzzBuildRejectsMalformedGeometry"),
    ("dynamic", "FuzzWorldUpdates"), ("dynamic", "FuzzCheckpointDecode"),
]
ROOT = Path("C:/Demiurgos/navmesh-validation/world-test").resolve()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def linux_path(path):
    text = str(path).replace("\\", "/")
    if text.startswith("/"):
        return text
    if len(text) > 2 and text[1:3] == ":/":
        return "/mnt/" + text[0].lower() + text[2:]
    raise ValueError("expected an absolute WSL or Windows path: " + text)


def host_toolchain(path):
    text = str(path)
    if text.startswith("/mnt/") and len(text) > 7 and text[6] == "/":
        return Path(text[5] + ":/" + text[7:])
    return Path(path)


def native_command(command, cwd, settings, platform, distro):
    if platform == "linux":
        return ["wsl", "-d", distro, "--cd", linux_path(cwd), "--exec", "env",
                *[key + "=" + value for key, value in settings.items()], *command]
    return command


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=("all", "prepare", "run"), default="all")
    parser.add_argument("--workspace", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--go")
    parser.add_argument("--platform", choices=("windows", "linux"), default="windows")
    parser.add_argument("--distro", default="Ubuntu")
    parser.add_argument("--seconds", type=int, default=600)
    parser.add_argument("--target", action="append", choices=[target for _, target in TARGETS])
    parser.add_argument("--retain-to", type=Path)
    parser.add_argument("--cohort", help="External coordinator record identifying concurrent target processes")
    args = parser.parse_args()
    output = args.output.resolve()
    if args.seconds <= 0 or not output.is_relative_to(ROOT):
        parser.error("use a positive duration and output below " + str(ROOT))
    manifest = output / "results.json"

    if args.phase != "run":
        if args.workspace is None or args.go is None:
            parser.error("prepare requires --workspace and --go")
        workspace = args.workspace.resolve()
        if not workspace.is_relative_to(ROOT):
            parser.error("workspace must be staged below " + str(ROOT))
        output.mkdir(parents=True, exist_ok=False)
        bin_dir = ROOT / "bin" / output.name / (args.platform + "-amd64")
        temp_dir = ROOT / "scratch" / output.name
        cache_dir = ROOT / "cache" / output.name
        for path in (bin_dir, temp_dir, cache_dir / "build", cache_dir / "modules", cache_dir / "fuzz", output / "logs", output / "failures"):
            path.mkdir(parents=True, exist_ok=False)
        # Fuzzing writes failures below testdata; never mutate the pinned stage.
        execution_source = temp_dir / "source"
        shutil.copytree(workspace, execution_source)
        path = linux_path if args.platform == "linux" else str
        settings = {"CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": "-mod=readonly", "GOTOOLCHAIN": "local", "GOENV": "off", "GOTELEMETRY": "off",
                    "GOOS": args.platform, "GOARCH": "amd64", "GOCACHE": path(cache_dir / "build"), "GOMODCACHE": path(cache_dir / "modules"),
                    "GOTMPDIR": path(temp_dir), "TEMP": path(temp_dir), "TMP": path(temp_dir), "TMPDIR": path(temp_dir)}
        env = os.environ.copy()
        env.update(settings if args.platform == "windows" else {key: str(temp_dir) for key in ("TEMP", "TMP", "TMPDIR")})
        env.pop("NAVMESH_RECAST_ORACLE", None)
        source_files = [{"path": p.relative_to(workspace).as_posix(), "sha256": digest(p)}
                        for p in sorted(workspace.rglob("*")) if p.is_file()]
        copied_files = [{"path": p.relative_to(execution_source).as_posix(), "sha256": digest(p)}
                        for p in sorted(execution_source.rglob("*")) if p.is_file()]
        if copied_files != source_files:
            raise ValueError("fuzz execution copy differs from the pinned input")
        version_command = native_command([args.go, "version"], workspace, settings, args.platform, args.distro)
        version = subprocess.check_output(version_command, cwd=workspace, env=env, text=True).strip()
        if not version.endswith(args.platform + "/amd64"):
            raise ValueError("toolchain does not execute natively for the selected platform: " + version)
        record = {"workspace": str(workspace), "execution_source": str(execution_source), "sources": source_files,
                  "seconds_per_target": args.seconds, "parallel_workers": 2, "environment": settings,
                  "platform": args.platform, "distro": args.distro if args.platform == "linux" else None,
                  "execution_source_verified": True, "host_temp": str(temp_dir),
                  "go": args.go, "go_sha256": digest(host_toolchain(args.go)),
                  "go_version": version, "version_command": version_command,
                  "harness_sha256": digest(Path(__file__)), "cohort": args.cohort,
                  "retain_to": str(args.retain_to.resolve()) if args.retain_to else None, "targets": []}
        for package, target in TARGETS:
            if args.target and target not in args.target:
                continue
            exe = bin_dir / (target + (".test.exe" if args.platform == "windows" else ".test"))
            # Compile-time -fuzz is required for coverage guidance.
            build = native_command([args.go, "test", "-c", "-fuzz=^" + target + "$", "-o", path(exe), "./" + package], workspace, settings, args.platform, args.distro)
            build_log = output / "logs" / (target + "-build.txt")
            with build_log.open("w", encoding="utf-8") as log:
                built = subprocess.run(build, cwd=workspace, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=600)
            item = {"package": package, "target": target, "build": build, "build_exit": built.returncode,
                    "build_log": str(build_log), "build_cwd": str(workspace), "execution_cwd": str(execution_source / package),
                    "executable": str(exe)}
            if built.returncode == 0:
                item["executable_sha256"] = digest(exe)
            record["targets"].append(item)
            manifest.write_text(json.dumps(record, indent=2) + "\n")
            if built.returncode:
                raise SystemExit(built.returncode)
            cache = cache_dir / "fuzz" / target
            cache.mkdir()
            item["command"] = native_command([path(exe), "-test.run=^$", "-test.fuzz=^" + target + "$", "-test.fuzztime=" + str(args.seconds) + "s",
                               "-test.parallel=2", "-test.timeout=" + str(args.seconds + 120) + "s", "-test.fuzzcachedir=" + path(cache)], execution_source / package, settings, args.platform, args.distro)
        record["prepared"] = True
        manifest.write_text(json.dumps(record, indent=2) + "\n")
        if args.phase == "prepare":
            print("Prepared coverage-instrumented binaries:", manifest, flush=True)
            return
    else:
        record = json.loads(manifest.read_text())

    if not record.get("prepared") or record.get("started"):
        parser.error("run requires a prepared, not-yet-started directory")
    for item in record["targets"]:
        if digest(Path(item["executable"])) != item["executable_sha256"]:
            parser.error("prepared executable changed: " + item["executable"])
    env = os.environ.copy()
    env.update(record["environment"] if record.get("platform", "windows") == "windows" else {key: record["host_temp"] for key in ("TEMP", "TMP", "TMPDIR")})
    env.pop("NAVMESH_RECAST_ORACLE", None)
    record["started"] = time.time()
    record["run_harness_sha256"] = digest(Path(__file__))
    manifest.write_text(json.dumps(record, indent=2) + "\n")
    for item in record["targets"]:
        target, package = item["target"], item["package"]
        log_path = output / "logs" / (target + ".txt")
        item["started"] = time.time()
        print(target, "starting", record["seconds_per_target"], "seconds", flush=True)
        with log_path.open("w", encoding="utf-8") as log:
            try:
                result = subprocess.run(item["command"], cwd=item["execution_cwd"], env=env, stdout=log, stderr=subprocess.STDOUT,
                                        timeout=record["seconds_per_target"] + 180)
                item["exit"] = result.returncode
            except subprocess.TimeoutExpired:
                item["exit"] = "harness-timeout"
        item["seconds"] = time.time() - item["started"]
        item["log"], item["log_sha256"] = str(log_path), digest(log_path)
        item["reproducers"] = []
        if item["exit"] != 0:
            corpus = Path(item["execution_cwd"]) / "testdata" / "fuzz" / target
            if corpus.is_dir():
                for source in sorted(corpus.iterdir()):
                    if not source.is_file():
                        continue
                    destination = output / "failures" / package / target / source.name
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(source, destination)
                    item["reproducers"].append({"path": str(destination), "sha256": digest(source)})
                    if record["retain_to"]:
                        retained = Path(record["retain_to"]) / package / target / source.name
                        retained.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copyfile(source, retained)
        manifest.write_text(json.dumps(record, indent=2) + "\n")
        print(target, "exit", item["exit"], flush=True)
    record["complete"] = True
    manifest.write_text(json.dumps(record, indent=2) + "\n")
    raise SystemExit(0 if all(item.get("exit") == 0 for item in record["targets"]) else 1)


if __name__ == "__main__":
    main()
