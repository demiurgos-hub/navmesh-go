"""Coordinate eight explicitly concurrent ten-minute fuzz targets.

Build all fixed, coverage-instrumented executables before any target starts.
Each target has its own scratch, build/fuzz/module caches, log, and manifest.
This is a correctness stress run, never a performance measurement.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

sys.dont_write_bytecode = True
from fuzz_sequential import ROOT, TARGETS


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--go", required=True)
    parser.add_argument("--platform", choices=("windows", "linux"), default="windows")
    parser.add_argument("--distro", default="Ubuntu")
    parser.add_argument("--seconds", type=int, default=600)
    parser.add_argument("--retain-to", type=Path)
    args = parser.parse_args()
    workspace, output = args.workspace.resolve(), args.output.resolve()
    if args.seconds <= 0 or not workspace.is_relative_to(ROOT) or not output.is_relative_to(ROOT):
        parser.error("use positive duration and paths below " + str(ROOT))
    output.mkdir(parents=True, exist_ok=False)
    manifest = output / "cohort.json"
    runner = Path(__file__).with_name("fuzz_sequential.py")
    env = os.environ.copy()
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    record = {"workspace": str(workspace), "seconds_per_target": args.seconds,
              "platform": args.platform, "distro": args.distro if args.platform == "linux" else None,
              "logical_cpus": os.cpu_count(), "maximum_concurrent_preparers": 2,
              "target_processes": len(TARGETS), "workers_per_target": 2, "maximum_fuzz_workers": len(TARGETS) * 2,
              "coordinator_processes": len(TARGETS), "timing_claim": "None; concurrent correctness stress only",
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "targets": []}

    def save():
        manifest.write_text(json.dumps(record, indent=2) + "\n")

    for package, target in TARGETS:
        lane = output.parent / (output.name + "-" + target)
        prepare = [sys.executable, str(runner), "--phase", "prepare", "--workspace", str(workspace), "--output", str(lane),
                   "--go", args.go, "--platform", args.platform, "--distro", args.distro,
                   "--seconds", str(args.seconds), "--target", target, "--cohort", str(manifest)]
        if args.retain_to:
            prepare += ["--retain-to", str(args.retain_to.resolve())]
        record["targets"].append({"package": package, "target": target, "output": str(lane), "prepare": prepare,
                                  "run": [sys.executable, str(runner), "--phase", "run", "--output", str(lane)]})
    save()

    def execute(item, phase):
        log_path = output / (item["target"] + "-" + phase + ".txt")
        started = time.time()
        with log_path.open("w", encoding="utf-8") as log:
            try:
                result = subprocess.run(item[phase], env=env, stdout=log, stderr=subprocess.STDOUT,
                                        timeout=1200 if phase == "prepare" else args.seconds + 240)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = "cohort-timeout"
        return {"exit": code, "started": started, "finished": time.time(), "log": str(log_path),
                "log_sha256": hashlib.sha256(log_path.read_bytes()).hexdigest()}

    print("Preparing eight coverage-instrumented targets; no fuzz processes running", flush=True)
    with ThreadPoolExecutor(max_workers=2) as pool:
        outcomes = list(pool.map(lambda item: execute(item, "prepare"), record["targets"]))
    for item, result in zip(record["targets"], outcomes):
        item["prepare_result"] = result
    save()
    if any(result["exit"] != 0 for result in outcomes):
        raise SystemExit(1)

    record["fuzz_phase_started"] = time.time()
    save()
    print("Starting eight concurrent targets, two workers each; no compilation or timing job", flush=True)
    with ThreadPoolExecutor(max_workers=len(TARGETS)) as pool:
        outcomes = list(pool.map(lambda item: execute(item, "run"), record["targets"]))
    for item, result in zip(record["targets"], outcomes):
        item["run_result"] = result
    record["fuzz_phase_finished"] = time.time()
    record["complete"] = True
    save()
    print(manifest, flush=True)
    raise SystemExit(0 if all(result["exit"] == 0 for result in outcomes) else 1)


if __name__ == "__main__":
    main()
