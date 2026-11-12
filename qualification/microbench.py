"""Prepare hashed baseline/candidate binaries, then run them in a quiet window.

The prepare command compiles only; run never compiles. Results are development
observations, not tail-latency, confidence-interval, or agent-capacity claims.
"""
import argparse
import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import time


ROOT = Path("C:/Demiurgos/navmesh-validation/world-test").resolve()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_record(root):
    files = [{"path": p.relative_to(root).as_posix(), "sha256": digest(p)}
             for p in sorted(root.rglob("*")) if p.is_file()]
    return {"path": str(root), "files": files,
            "sha256": hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()}


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    prepare = sub.add_parser("prepare")
    prepare.add_argument("--baseline", required=True, type=Path)
    prepare.add_argument("--candidate", required=True, type=Path)
    prepare.add_argument("--output", required=True, type=Path)
    prepare.add_argument("--go", required=True, type=Path)
    prepare.add_argument("--repository", type=Path, default=Path("C:/Demiurgos/navmesh-go"))
    prepare.add_argument("--baseline-head", default="7019eb5")
    execute = sub.add_parser("run")
    execute.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    output = args.output.resolve()
    if not output.is_relative_to(ROOT):
        parser.error("output must be below " + str(ROOT))
    manifest = output / "results.json"

    if args.command == "prepare":
        baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
        if not baseline.is_relative_to(ROOT) or not candidate.is_relative_to(ROOT):
            parser.error("baseline and candidate must be staged below " + str(ROOT))
        baseline_head = subprocess.check_output(["git", "-C", str(args.repository), "rev-parse", args.baseline_head], text=True).strip()
        archive = subprocess.check_output(["git", "-C", str(args.repository), "archive", baseline_head])
        verified = 0
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            for entry in tar:
                if not entry.isfile():
                    continue
                source = baseline / entry.name
                if not source.is_file() or source.read_bytes() != tar.extractfile(entry).read():
                    parser.error("baseline differs from pinned commit: " + entry.name)
                verified += 1
        fixture = "internal/testworld/world.go"
        if digest(baseline / fixture) != digest(candidate / fixture):
            parser.error("baseline/candidate benchmark fixture differs")
        output.mkdir(parents=True, exist_ok=False)
        bin_dir = ROOT / "bin" / output.name / "windows-amd64"
        temp_dir = ROOT / "scratch" / output.name
        cache_dir = ROOT / "cache" / output.name
        for path in (bin_dir, temp_dir, cache_dir / "build", cache_dir / "modules", output / "logs"):
            path.mkdir(parents=True, exist_ok=False)
        settings = {"CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": "-mod=readonly", "GOTOOLCHAIN": "local", "GOENV": "off", "GOTELEMETRY": "off",
                    "GOCACHE": str(cache_dir / "build"), "GOMODCACHE": str(cache_dir / "modules"),
                    "GOTMPDIR": str(temp_dir), "TEMP": str(temp_dir), "TMP": str(temp_dir), "TMPDIR": str(temp_dir), "GOOS": "windows", "GOARCH": "amd64"}
        env = os.environ.copy()
        env.update(settings)
        record = {"prepared_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "baseline_head": baseline_head,
                  "baseline_verified_files": verified, "sources": {"baseline": source_record(baseline), "candidate": source_record(candidate)},
                  "fixture_sha256": digest(candidate / fixture), "environment": settings,
                  "go": str(args.go), "go_sha256": digest(args.go), "harness_sha256": digest(Path(__file__)),
                  "go_version": subprocess.check_output([str(args.go), "version"], env=env, text=True).strip(),
                  "platform": platform.platform(), "processor": platform.processor(), "logical_cpus": os.cpu_count(),
                  "builds": [], "runs": [], "exclusions": "Small fixed fixture, sequential queries; no capacity or tail-latency claim."}
        for label, source in (("baseline", baseline), ("candidate", candidate)):
            binary = bin_dir / (label + ".test.exe")
            command = [str(args.go), "test", "-c", "-o", str(binary), "."]
            log = output / "logs" / (label + "-build.txt")
            with log.open("w", encoding="utf-8") as stream:
                result = subprocess.run(command, cwd=source, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=600)
            item = {"label": label, "command": command, "cwd": str(source), "exit": result.returncode, "log": str(log), "binary": str(binary)}
            if result.returncode == 0:
                item["sha256"] = digest(binary)
            record["builds"].append(item)
            manifest.write_text(json.dumps(record, indent=2) + "\n")
            if result.returncode:
                raise SystemExit(result.returncode)
        record["prepared"] = True
        manifest.write_text(json.dumps(record, indent=2) + "\n")
        print("Prepared only; schedule run separately:", output, flush=True)
        return

    record = json.loads(manifest.read_text())
    if not record.get("prepared") or record.get("runs"):
        parser.error("run requires a prepared, not-yet-run result directory")
    env = os.environ.copy()
    env.update(record["environment"])
    for item in record["builds"]:
        binary = Path(item["binary"])
        if digest(binary) != item["sha256"]:
            parser.error("prepared executable changed: " + str(binary))
    for item in record["builds"]:
        names = "Projection|Path|SurfaceMove"
        if item["label"] == "candidate":
            names += "|SurfaceMoveWithoutVisited|RepairPath|QueryCreation"
        command = [item["binary"], "-test.run=^$", "-test.bench=^Benchmark(" + names + ")$", "-test.benchmem", "-test.benchtime=500ms", "-test.count=3"]
        log = output / "logs" / (item["label"] + "-benchmark.txt")
        started = time.time()
        print(item["label"], "benchmark starting", flush=True)
        with log.open("w", encoding="utf-8") as stream:
            result = subprocess.run(command, cwd=item["cwd"], env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=600)
        record["runs"].append({"label": item["label"], "command": command, "cwd": item["cwd"], "exit": result.returncode,
                               "seconds": time.time() - started, "log": str(log), "log_sha256": digest(log)})
        manifest.write_text(json.dumps(record, indent=2) + "\n")
        if result.returncode:
            raise SystemExit(result.returncode)
    record["complete"] = True
    manifest.write_text(json.dumps(record, indent=2) + "\n")
    print(manifest, flush=True)


if __name__ == "__main__":
    main()
