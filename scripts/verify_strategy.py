"""Run chapter-18 L1/L2 adapters; never count absent L3 evidence as a pass."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from collections import Counter


def verify(root, output):
    source = root / "docs/作战策略.md"
    data = source.read_bytes()
    fixtures = []
    for block in re.findall(r"```json\s*\n(.*?)\n```", data.decode("utf-8"), re.S):
        item = json.loads(block)
        if isinstance(item, dict) and re.fullmatch(r"S\d\d", str(item.get("id", ""))):
            if not {"id", "module", "requires", "input", "expected", "negative"} <= item.keys():
                raise ValueError("Incomplete fixture: " + item["id"])
            fixtures.append(item)
    ids = [x["id"] for x in fixtures]
    if len(ids) != len(set(ids)) or not ids:
        raise ValueError("Missing fixtures or duplicate IDs")
    frozen = json.loads((root / "internal/game/testdata/strategy_scenarios.json").read_text(encoding="utf-8"))
    if frozen != fixtures:
        raise ValueError("Strategy fixtures changed: review adapters and update the frozen fixture file before running")

    env = dict(os.environ, GO111MODULE="on")
    proc = subprocess.run(["go", "test", "./internal/game", "-run", "^TestStrategyS", "-count=1", "-json"],
                          cwd=root, env=env, capture_output=True, text=True, encoding="utf-8", errors="replace")
    results, logs = {}, {}
    for line in proc.stdout.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        name = event.get("Test", "")
        if not name:
            continue
        if event.get("Output"):
            logs.setdefault(name, []).append(event["Output"])
        if event.get("Action") in ("pass", "fail", "skip"):
            results[name] = event["Action"]
    records = []
    for fixture in fixtures:
        name = "TestStrategy" + fixture["id"]
        missing = [x for x in fixture["requires"] if x.startswith("VERIFIED:")]
        record = dict(id=fixture["id"], module=fixture["module"], level="L1/L2",
                      requirements=fixture["requires"], expected=fixture["expected"],
                      negative=fixture["negative"], log="".join(logs.get(name, [])))
        if missing:
            record.update(status="BLOCKED", reason="No versioned official experiment evidence: " + ", ".join(missing))
            # A is independently executable; B still requires a verified cycle.
            if fixture["id"] == "S10":
                record["partial_checks"] = {"A": results.get(name + "A", "NOT_RUN"), "log": "".join(logs.get(name + "A", []))}
        elif results.get(name) == "pass":
            record.update(status="PASS", reason="Production selector/model adapter and negative case executed; not an L3 result")
        elif results.get(name) == "fail":
            record.update(status="FAIL", reason="Adapter assertion failed")
        else:
            record.update(status="NOT_RUN", reason="No completed adapter; absent tests and skips are not passes")
        records.append(record)
    summary = {key: 0 for key in ("PASS", "FAIL", "BLOCKED", "NOT_RUN")}
    summary.update(Counter(x["status"] for x in records))
    report = dict(strategy_sha256=hashlib.sha256(data).hexdigest(), rule_version="document-" + hashlib.sha256(data).hexdigest()[:12],
                  official_judge_version=None, summary=summary, records=records,
                  tool_exit_code=proc.returncode, tool_stderr=proc.stderr,
                  limitation="Finite synthetic fixtures; no official battle, timing evidence or global optimality claim")
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False))
    print(output)
    return 1 if proc.returncode or summary["FAIL"] or summary["NOT_RUN"] else 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path("dist/strategy-report.json"))
    args = parser.parse_args()
    sys.exit(verify(Path(__file__).resolve().parents[1], args.output.resolve()))
