"""Verify archive layout, isolated Go builds/tests and direct executable startup."""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import socket
import re
import subprocess
import tarfile
import tempfile
import time
import urllib.request


def verify(archive_path, local_go=False):
    with tempfile.TemporaryDirectory(prefix="coregeek-check-") as temp:
        with tarfile.open(archive_path, "r:gz") as archive:
            names = set()
            for member in archive.getmembers():
                path = PurePosixPath(member.name)
                if (not member.isfile() or path.parts[:2] != ("CoreGeek", "src")
                        or ".." in path.parts or member.name in names):
                    raise ValueError(f"invalid archive member: {member.name}")
                names.add(member.name)
            required = {"main.go", "go.mod", "Makefile", "run.sh", "configs/default.json",
                        "internal/protocol/testdata/request.json"}
            assert all("CoreGeek/src/" + name in names for name in required)
            script = archive.getmember("CoreGeek/src/run.sh")
            assert script.mode & 0o111
            assert b"\r" not in archive.extractfile(script).read()
            assert b"go 1.24.7" in archive.extractfile("CoreGeek/src/go.mod").read()
            archive.extractall(temp, filter="data")
        root = Path(temp) / "CoreGeek" / "src"
        env = dict(os.environ, GO111MODULE="on", CGO_ENABLED="0", GOTOOLCHAIN="auto")
        for name in ("GOOS", "GOARCH", "AGENT_CONFIG", "GOFLAGS", "GOWORK"):
            env.pop(name, None)
        env["GOWORK"] = "off"
        if local_go:
            env["GOTOOLCHAIN"] = "local"
            version = subprocess.check_output(["go", "env", "GOVERSION"], env=env, text=True).strip()
            mod = root / "go.mod"
            mod.write_text(re.sub(r"(?m)^go .+$", "go " + version.removeprefix("go"), mod.read_text()))
            print("LOCAL CHECK ONLY: temporary extracted go.mod uses " + version +
                  "; original archive and local project are unchanged.", flush=True)
        binary = "main.exe" if os.name == "nt" else "main"
        for args in (["go", "version"], ["go", "test", "./..."], ["go", "vet", "./..."],
                     ["go", "build", "-trimpath", "-o", binary, "main.go"]):
            subprocess.run(args, cwd=root, env=env, check=True)
        subprocess.run(["go", "build", "-trimpath", "-o", str(Path(temp) / "linux-main"), "main.go"],
                       cwd=root, env=dict(env, GOOS="linux", GOARCH="amd64"), check=True)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        # Start outside the source directory to also verify executable-relative config lookup.
        process = subprocess.Popen([str(root / binary), str(port)], cwd=temp,
                                   env=dict(env, AGENT_DEBUG="false"))
        try:
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            url = f"http://127.0.0.1:{port}"
            for _ in range(100):
                if process.poll() is not None:
                    raise RuntimeError("executable exited before listening")
                try:
                    with opener.open(url + "/healthz", timeout=1) as response:
                        assert json.load(response)["status"] == "ok"
                    break
                except OSError:
                    time.sleep(0.1)
            else:
                raise TimeoutError("service did not listen on the supplied port")
            payload = (root / "internal/protocol/testdata/request.json").read_bytes()
            request = urllib.request.Request(url + "/", data=payload,
                                             headers={"Content-Type": "application/json"})
            with opener.open(request, timeout=5) as response:
                assert "roleCommandMap" in json.load(response)
            print("PASS: archive layout, isolated tests/vet, native/Linux builds, direct HTTP port and POST")
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive")
    parser.add_argument("--local-go", action="store_true",
                        help="check with installed Go using a modified temporary copy; not platform toolchain validation")
    options = parser.parse_args()
    verify(Path(options.archive).resolve(), options.local_go)
