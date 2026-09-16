"""Build the platform source archive using only the submission whitelist."""
import argparse
import io
from pathlib import Path
import re
import tarfile

SRC_FILES = {"go.mod", "go.sum", "main.go", "run.sh", "Makefile"}
SRC_DIRS = {"cmd", "configs", "internal", "scripts"}
SKIP_DIRS = {"Demo", "demo", "Docs", "docs", "__pycache__"}


def package(root, output, go_version="1.24.7"):
    root, output = Path(root).resolve(), Path(output).resolve()
    if not re.fullmatch(r"\d+\.\d+\.\d+", go_version):
        raise ValueError("Go version must have the form major.minor.patch")
    required = {"go.mod", "main.go", "run.sh", "Makefile", "configs/default.json"}
    files = []
    for path in sorted(root.rglob("*")):
        rel = path.relative_to(root)
        if any(p.startswith(".") or p in SKIP_DIRS for p in rel.parts[:-1]):
            continue
        if not (rel.as_posix() in SRC_FILES or rel.parts[0] in SRC_DIRS):
            continue
        if path.is_symlink():
            raise ValueError(f"symlinks are not supported: {rel}")
        if not path.is_file() or path.name.startswith(".") or path.suffix.lower() == ".md":
            continue
        if path.suffix.lower() in {".pyc", ".exe"} or path == output:
            continue
        files.append((path, rel.as_posix()))
    missing = required - {rel for _, rel in files}
    if missing:
        raise ValueError(f"missing required source files: {sorted(missing)}")
    output.parent.mkdir(parents=True, exist_ok=True)
    # Build in memory so a validation error cannot leave a partial final archive.
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w:gz") as archive:
        for path, rel in files:
            data = path.read_bytes()
            if rel == "go.mod":
                data, count = re.subn(rb"(?m)^go\s+\d+\.\d+(?:\.\d+)?\s*$",
                                      b"go " + go_version.encode() + b"\n", data)
                if count != 1:
                    raise ValueError("go.mod must contain exactly one Go version directive")
            if path.suffix == ".sh" or rel == "Makefile":
                data = data.replace(b"\r\n", b"\n")
            info = tarfile.TarInfo("CoreGeek/src/" + rel)
            info.size = len(data)
            info.mode = 0o755 if path.suffix == ".sh" else 0o644
            archive.addfile(info, io.BytesIO(data))
    output.write_bytes(buffer.getvalue())
    return len(files)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default="CoreGeek.tar.gz")
    parser.add_argument("--go-version", default="1.24.7")
    args = parser.parse_args()
    count = package(Path(__file__).resolve().parent, args.output, args.go_version)
    print(f"Packaged {count} files into {Path(args.output).resolve()}")
