"""Exercise a packaged Linux service over real HTTP, including SIGTERM flushing."""
import json
import argparse
import hashlib
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
import zipfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("archive")
    parser.add_argument("fixture")
    parser.add_argument("output")
    parser.add_argument("--no-debug", action="store_true")
    args = parser.parse_args()
    archive, fixture, output = map(lambda s: Path(s).resolve(), (args.archive, args.fixture, args.output))
    output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix="competition-smoke-") as temp:
        root = Path(temp)
        with zipfile.ZipFile(archive) as z:
            for entry in z.infolist():
                target = (root / entry.filename).resolve()
                if not target.is_relative_to(root):
                    raise ValueError("ZIP entry outside root")
                z.extract(entry, root)
                target.chmod((entry.external_attr >> 16) & 0o777 or 0o600)
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        env = dict(os.environ)
        env.pop("AGENT_CONFIG", None)
        env["AGENT_DEBUG"] = "false" if args.no_debug else "true"
        with (output / "stderr.txt").open("wb") as err:
            proc = subprocess.Popen(["bash", str(root / "run.sh"), str(port)], cwd=root,
                                    env=env, stdout=subprocess.DEVNULL, stderr=err)
            try:
                url = f"http://127.0.0.1:{port}"
                for attempt in range(100):
                    if proc.poll() is not None:
                        raise RuntimeError("service exited before health check")
                    try:
                        with urllib.request.urlopen(url + "/healthz", timeout=1) as r:
                            assert json.load(r)["status"] == "ok"
                        break
                    except OSError:
                        time.sleep(0.05)
                else:
                    raise TimeoutError("service not ready")
                raw = fixture.read_bytes()
                replies = []
                for _ in range(2):
                    req = urllib.request.Request(url + "/", data=raw,
                                                 headers={"Content-Type": "application/json"})
                    with urllib.request.urlopen(req, timeout=5) as r:
                        assert r.status == 200
                        replies.append(r.read())
                assert replies[0] == replies[1], "duplicate response changed"
                assert "roleCommandMap" in json.loads(replies[0])
                if args.no_debug:
                    req = urllib.request.Request(url + "/", data=b"{broken",
                                                 headers={"Content-Type": "application/json"})
                    with urllib.request.urlopen(req, timeout=5) as r:
                        assert "roleCommandMap" in json.load(r)
                proc.send_signal(signal.SIGTERM)
                assert proc.wait(timeout=10) == 0, "SIGTERM was not graceful"
            finally:
                if proc.poll() is None:
                    proc.kill()
                    proc.wait()
        records = sorted((root / "logs").glob("*/*.json"))
        if args.no_debug:
            assert not (root / "logs").exists(), "disabled logs created a directory"
            console = (output / "stderr.txt").read_text()
            assert "level=INFO" not in console and "level=DEBUG" not in console, console
            assert "turn_failed" in console and "decode_error" in console, console
        else:
            assert len(records) == 2, f"missing flushed records: {records}"
            outcomes = sorted(json.loads(p.read_text())["outcome"] for p in records)
            assert outcomes == ["cache_hit", "decided"], outcomes
            shutil.copytree(records[0].parent, output / "records")
        result = {"health": "ok", "httpStatus": 200, "idempotent": True,
                  "sigtermExitCode": 0, "flushedRecords": len(records),
                  "debug": not args.no_debug,
                  "responseSHA256": hashlib.sha256(replies[0]).hexdigest(),
                  "scope": "local Linux service, not official judge"}
        (output / "result.json").write_text(json.dumps(result, indent=2))
        print(json.dumps(result))


if __name__ == "__main__":
    main()
