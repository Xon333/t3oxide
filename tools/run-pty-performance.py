#!/usr/bin/env python3
"""Run source-equivalent actual-program Linux PTY observations.

Each Go invocation is a new process and creates fresh actual Node helpers.
The baseline overlay adds only these tests and a held-read counter to its
synthetic server. Baseline production source is never edited.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import shlex
import subprocess
import time


def write_json(path, value):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(path)


def distribution(values):
    ordered = sorted(values)
    if not ordered:
        return {"n": 0}
    pick = lambda p: ordered[max(0, math.ceil(p * len(ordered)) - 1)]
    return {"n": len(ordered), "median": pick(.5), "p95": pick(.95),
            "p99": pick(.99), "worst": ordered[-1], "minimum": ordered[0]}


def parse_record(path):
    records = []
    for line in path.read_text().splitlines():
        offset = line.find('{"')
        if offset >= 0:
            value = json.loads(line[offset:])
            if "rawStages" in value or "goStages" in value:
                records.append(value)
    if len(records) != 1:
        raise ValueError(f"expected one measurement record in {path}, got {len(records)}")
    return records[0]


def source_digest(source):
    entries = []
    for path in sorted(source.rglob("*")):
        if not path.is_file() or ".git" in path.parts:
            continue
        relative = path.relative_to(source).as_posix()
        if (path.suffix == ".go" and not path.name.endswith("_test.go")) or relative in ("go.mod", "go.sum") or relative.startswith("adapter/"):
            entries.append({"path": relative, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
    return {"sha256": hashlib.sha256(json.dumps(entries, separators=(",", ":")).encode()).hexdigest(), "entries": entries}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["before", "after"])
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--go", default="go")
    parser.add_argument("--runs", type=int, default=3)
    parser.add_argument("--samples", type=int, default=1000)
    args = parser.parse_args()
    args.source, args.baseline, args.report = (p.resolve() for p in (args.source, args.baseline, args.report))
    if not 2 <= args.samples <= 1000 or args.runs < 1:
        parser.error("samples must be 2..1000 and runs at least 1")
    args.report.mkdir(parents=True, exist_ok=True)
    logs = args.report / "logs"
    logs.mkdir(exist_ok=True)
    target = args.baseline if args.phase == "before" else args.source
    prefix = f"adapter-pty-{args.phase}"
    # All output paths must be fresh so failed or earlier evidence is retained.
    if any(logs.glob(prefix + "-fast-1-*.log")):
        parser.error("phase logs already exist; use a new report directory to preserve them")
    overlay = args.report / "adapter-pty-baseline-overlay.json"
    if args.phase == "before":
        original = (args.baseline / "internal/integration/fixture_test.go").read_text()
        field = "\tshellHold       <-chan struct{}"
        branch = "\t\tif hold != nil {\n\t\t\tselect {"
        if original.count(field) != 1 or original.count(branch) != 1:
            raise ValueError("baseline fixture identity/shape changed; review instrumentation overlay")
        observed = original.replace(field, field + "\n\tactiveShellHolds int").replace(branch,
            "\t\tif hold != nil {\n\t\t\tf.mu.Lock()\n\t\t\tf.activeShellHolds++\n\t\t\tf.mu.Unlock()\n"
            "\t\t\tdefer func() { f.mu.Lock(); f.activeShellHolds--; f.mu.Unlock() }()\n\t\t\tselect {")
        fixture = args.report / "adapter-baseline-fixture-observation.go"
        fixture.write_text(observed)
        replacements = {str(args.baseline / "internal/integration/fixture_test.go"): str(fixture)}
        for name in ["performance_pty_linux_test.go", "performance_stages_linux_test.go"]:
            replacements[str(args.baseline / "internal/integration" / name)] = str(args.source / "internal/integration" / name)
        write_json(overlay, {"Replace": replacements})
    environment = os.environ.copy()
    environment.update(T3OXIDE_PTY_SAMPLES=str(args.samples), T3OXIDE_STAGE_HARNESS_ROOT=str(args.source))
    environment.pop("T3OXIDE_PTY_STREAM", None)
    environment.pop("T3OXIDE_PTY_HOSTS", None)
    checks, results = [], []
    common = [args.go, "test"]
    if args.phase == "before":
        common.append(f"-overlay={overlay}")
    common += ["./internal/integration", "-count=1", "-v"]

    def run(name, number, test, extra):
        path = logs / f"adapter-{name}-{args.phase}-{number}.log"
        if path.exists():
            raise ValueError(f"refusing to overwrite retained log {path}")
        env = environment | extra
        command = common + ["-run", "^" + test + "$"]
        start = time.monotonic()
        with path.open("w") as output:
            completed = subprocess.run(command, cwd=target, env=env, stdout=output, stderr=subprocess.STDOUT)
        check = {"id": path.stem, "command": shlex.join(command), "environment": {k: env[k] for k in ["GOCACHE", "T3OXIDE_PTY_SAMPLES", "T3OXIDE_PTY_HOSTS", "T3OXIDE_PTY_STREAM", "T3OXIDE_STAGE_HARNESS_ROOT"] if k in env},
                 "cwd": str(target), "exitCode": completed.returncode, "log": "logs/" + path.name,
                 "wallSeconds": time.monotonic() - start, "boundary": "fresh Go/Node process; actual Linux PTY; synthetic source; no paint/physical input"}
        checks.append(check)
        write_json(args.report / f"adapter-pty-checks-{args.phase}.json", checks)
        result = {"name": name, "run": number, "log": check["log"], "exitCode": completed.returncode}
        if completed.returncode == 0:
            result["record"] = parse_record(path)
        results.append(result)
        print(f"{args.phase} {name} independent process {number}: exit {completed.returncode}", flush=True)

    for number in range(1, args.runs + 1):
        for name, hosts, stream in [("fast-1", 1, ""), ("held-2", 2, ""), ("partial-4", 4, ""), ("stream-search", 1, "mixed-search")]:
            run("pty-" + name, number, "TestPerformanceActualPTYAdmissionToWrite", {"T3OXIDE_PTY_HOSTS": str(hosts), "T3OXIDE_PTY_STREAM": stream})
        run("stages", number, "TestPerformanceActualOpeningAndRequestStages", {})
    report = {"schema": 1, "phase": args.phase, "runs": args.runs, "samplesPerRun": args.samples,
              "toolchain": {"go": subprocess.check_output([args.go, "version"], text=True).strip(), "node": subprocess.check_output(["node", "--version"], text=True).strip(), "system": platform.platform()},
              "source": source_digest(target), "raw": results, "failures": sum(result["exitCode"] != 0 for result in results),
              "boundary": "fresh helper processes, warm OS/compiler/module caches; loopback fixture; no SSH, emulator paint or physical input; master drained; first kernel write after prepared View may include housekeeping"}
    grouped = {}
    for result in results:
        if result["exitCode"] or "rawStages" not in result.get("record", {}):
            continue
        stages = result["record"]["rawStages"]
        destination = grouped.setdefault(result["name"], {"runs": [], "values": {}})
        destination["runs"].append({key: distribution([stage[key] for stage in stages]) for key in stages[0] if key != "writeBytes"})
        for stage in stages:
            for key, value in stage.items():
                if key != "writeBytes":
                    destination["values"].setdefault(key, []).append(value)
    report["distributions"] = {name: {"pooled": {key: distribution(values) for key, values in result["values"].items()}, "independentProcesses": result["runs"]} for name, result in grouped.items()}
    write_json(args.report / f"adapter-pty-performance-{args.phase}.json", report)
    if args.phase == "before" and report["failures"] == 0:
        budgets = {"schema": 1, "setFrom": "completed before-phase independent-process measurements, before running after-phase", "rationale": "Per metric, worst independent baseline-process percentile plus 10% and 1ms replay tolerance for shared cloud scheduling/60fps quantization. This is a measured fixture regression gate, not a product latency promise; exact distributions and ratios remain visible.", "metrics": {}}
        for name, result in grouped.items():
            budgets["metrics"][name] = {}
            for metric in ["admissionToUpdateMs", "admissionToFirstKernelWriteMs"]:
                budgets["metrics"][name][metric] = {percentile: max(run[metric][percentile] for run in result["runs"]) * 1.1 + 1 for percentile in ["p95", "p99"]}
        write_json(args.report / "adapter-pty-budgets.json", budgets)
    if args.phase == "after" and report["failures"] == 0:
        budgets = json.loads((args.report / "adapter-pty-budgets.json").read_text())
        gates = []
        for name, metrics in budgets["metrics"].items():
            for metric, limits in metrics.items():
                for percentile, limit in limits.items():
                    observed = report["distributions"][name]["pooled"][metric][percentile]
                    gates.append({"scenario": name, "metric": metric, "percentile": percentile, "limitMs": limit, "observedMs": observed, "pass": observed <= limit})
        write_json(args.report / "adapter-pty-regression-gates.json", {"gates": gates, "pass": all(gate["pass"] for gate in gates)})
        if not all(gate["pass"] for gate in gates):
            return 1
    return 1 if report["failures"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
