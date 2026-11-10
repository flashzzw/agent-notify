#!/usr/bin/env python3
"""Build the same standalone release archives locally and in GitHub Actions."""

import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("windows-amd64", "windows-arm64", "linux-amd64", "linux-arm64")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="dev")
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", args.version):
        parser.error("version must contain only letters, digits, dots, underscores or hyphens")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    checksums = []

    with tempfile.TemporaryDirectory(prefix="agent-notify-package-") as tmp:
        for target in TARGETS:
            goos, goarch = target.split("-")
            binary_name = "agent-notify.exe" if goos == "windows" else "agent-notify"
            binary = Path(tmp) / binary_name
            env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED="0")
            subprocess.run(
                ["go", "build", "-trimpath", "-ldflags",
                 f"-s -w -X github.com/flashzzw/agent-notify/internal/cli.Version={args.version}",
                 "-o", str(binary), "./cmd/agent-notify"],
                cwd=ROOT, env=env, check=True,
            )
            files = [(binary, binary_name)] + [
                (ROOT / name, name) for name in ("README.md", "README.zh-CN.md", "LICENSE")
            ]
            # Keep README links valid after extraction as well as on GitHub.
            files += [(path, str(path.relative_to(ROOT))) for path in sorted((ROOT / "docs").rglob("*")) if path.is_file()]
            suffix = ".zip" if goos == "windows" else ".tar.gz"
            archive = output / f"agent-notify-{args.version}-{target}{suffix}"
            if goos == "windows":
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as package:
                    for path, name in files:
                        package.write(path, name)
            else:
                with tarfile.open(archive, "w:gz") as package:
                    for path, name in files:
                        package.add(path, arcname=name)
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksums.append(f"{digest}  {archive.name}\n")
            print(archive.name, flush=True)

    # Only include archives from this invocation, even if output has older releases.
    (output / "SHA256SUMS").write_text("".join(checksums), encoding="utf-8")


if __name__ == "__main__":
    main()
