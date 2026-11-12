"""Build and run the optional, pinned standalone C++ oracle; never enable cgo."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


ROOT = Path("C:/Demiurgos/navmesh-validation/world-test").resolve()
PIN = "9f4ce64458dfae86e1239c525ddc219c4e9e06f1"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--reference", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--go", required=True, type=Path)
    parser.add_argument("--compiler", required=True, type=Path)
    args = parser.parse_args()
    workspace, output, reference = args.workspace.resolve(), args.output.resolve(), args.reference.resolve()
    if not workspace.is_relative_to(ROOT) or not output.is_relative_to(ROOT):
        parser.error("workspace/output must be below " + str(ROOT))
    pin = subprocess.check_output(["git", "-C", str(reference), "rev-parse", "HEAD"], text=True).strip()
    if pin != PIN or subprocess.check_output(["git", "-C", str(reference), "status", "--porcelain"], text=True).strip():
        parser.error("reference must be a clean checkout of " + PIN)
    output.mkdir(parents=True, exist_ok=False)
    bin_dir = ROOT / "bin" / output.name / "windows-amd64"
    temp_dir = ROOT / "scratch" / output.name
    cache_dir = ROOT / "cache" / output.name
    for path in (bin_dir, temp_dir, cache_dir / "build", cache_dir / "modules", output / "logs"):
        path.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    env.update({"CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": "-mod=readonly", "GOTOOLCHAIN": "local", "GOENV": "off", "GOTELEMETRY": "off",
                "GOCACHE": str(cache_dir / "build"), "GOMODCACHE": str(cache_dir / "modules"),
                "GOTMPDIR": str(temp_dir), "TEMP": str(temp_dir), "TMP": str(temp_dir), "TMPDIR": str(temp_dir)})
    source_files = sorted(p for name in ("Recast", "Detour") for p in (reference / name).rglob("*") if p.suffix in (".cpp", ".h"))
    record = {"workspace": str(workspace), "candidate_sources": [
                  {"path": p.relative_to(workspace).as_posix(), "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
                  for p in sorted(workspace.rglob("*")) if p.is_file()],
              "environment": {key: env[key] for key in ("CGO_ENABLED", "GOWORK", "GOFLAGS", "GOTOOLCHAIN", "GOENV", "GOTELEMETRY", "GOCACHE", "GOMODCACHE", "GOTMPDIR", "TEMP", "TMP", "TMPDIR")},
              "reference_commit": pin, "reference_tree": subprocess.check_output(["git", "-C", str(reference), "rev-parse", "HEAD^{tree}"], text=True).strip(),
              "sources": [{"path": str(p.relative_to(reference)), "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in source_files],
              "runner_sha256": hashlib.sha256((workspace / "recast/testdata/oracle/main.cpp").read_bytes()).hexdigest(),
              "compiler": str(args.compiler), "compiler_sha256": hashlib.sha256(args.compiler.read_bytes()).hexdigest(),
              "go": str(args.go), "go_sha256": hashlib.sha256(args.go.read_bytes()).hexdigest(),
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "CGO_ENABLED": "0", "commands": []}

    def run(command, cwd=workspace):
        log = output / "logs" / (str(len(record["commands"])).zfill(3) + ".txt")
        started = time.time()
        with log.open("w", encoding="utf-8") as stream:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=600)
        record["commands"].append({"argv": [str(x) for x in command], "cwd": str(cwd), "exit": result.returncode, "seconds": time.time() - started, "log": str(log)})
        (output / "results.json").write_text(json.dumps(record, indent=2) + "\n")
        if result.returncode:
            print(log.read_text(encoding="utf-8"))
            raise SystemExit(result.returncode)

    run([str(args.compiler), "--version"])
    run([str(args.go), "version"])
    oracle = bin_dir / "navmesh-recast-oracle.exe"
    compile_sources = [p for p in source_files if p.suffix == ".cpp"]
    run([str(args.compiler), "-std=c++11", "-O2", "-I", str(reference / "Recast/Include"), "-I", str(reference / "Detour/Include"),
         "recast/testdata/oracle/main.cpp", *[str(p) for p in compile_sources], "-o", str(oracle)])
    record["oracle_sha256"] = hashlib.sha256(oracle.read_bytes()).hexdigest()
    test = bin_dir / "recast-oracle.test.exe"
    run([str(args.go), "test", "-c", "-o", str(test), "./recast"])
    record["test_executable_sha256"] = hashlib.sha256(test.read_bytes()).hexdigest()
    env["NAVMESH_RECAST_ORACLE"] = str(oracle)
    env["NAVMESH_ORACLE_ARTIFACTS"] = str(ROOT / "worlds" / output.name / "oracle")
    record["comparison_artifacts"] = env["NAVMESH_ORACLE_ARTIFACTS"]
    run([str(test), "-test.run=^TestUpstreamOracle$", "-test.v", "-test.count=1"], cwd=workspace / "recast")
    record["complete"] = True
    (output / "results.json").write_text(json.dumps(record, indent=2) + "\n")
    print(output / "results.json", flush=True)


if __name__ == "__main__":
    main()
