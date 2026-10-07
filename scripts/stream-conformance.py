#!/usr/bin/env python3
"""Test checked-out routing/bridge code with current and released source contracts."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SOURCE_MODULE = "github.com/skosovsky/prompty"


def run(*args, cwd, env):
    print("+", " ".join(map(str, args)), flush=True)
    subprocess.run(args, cwd=cwd, env=env, check=True)


def main():
    env = os.environ.copy()
    env["GOWORK"] = "off"
    with tempfile.TemporaryDirectory(prefix="routery-stream-") as temporary:
        work = Path(temporary).resolve()
        checkout = work / "routery"
        shutil.copytree(ROOT, checkout, ignore=shutil.ignore_patterns(
            ".git", ".cursor", "coverage.out", "go.work", "go.work.sum"))
        bridge = checkout / "ext" / "prompty"
        (bridge / "go.mod").unlink()
        (bridge / "go.sum").unlink(missing_ok=True)
        # The new bridge lives in the copied root module; no local replacement can
        # accidentally bypass the published source contract.
        released = subprocess.check_output(
            ["go", "list", "-m", "-f", "{{.Version}}", SOURCE_MODULE],
            cwd=ROOT / "ext" / "prompty", env=env, text=True).strip()
        run("go", "mod", "edit", f"-require={SOURCE_MODULE}@{released}", cwd=checkout, env=env)
        run("go", "mod", "tidy", cwd=checkout, env=env)
        replace = subprocess.check_output(
            ["go", "list", "-m", "-f", "{{if .Replace}}REPLACED{{end}}", SOURCE_MODULE],
            cwd=checkout, env=env, text=True).strip()
        if replace:
            raise RuntimeError("published source has a replacement")
        print(f"Published source: {released}; checked-out routing/bridge", flush=True)
        run("go", "test", "-race", "-count=1", "-timeout=90s",
            "./...", cwd=checkout, env=env)
        configured = os.environ.get("STREAM_SOURCE_DIR")
        if configured:
            source = Path(configured).resolve()
        else:
            source = work / "source"
            reference = os.environ.get("STREAM_SOURCE_REF") or (
                ROOT / "ext" / "prompty" / "conformance-source-ref.txt").read_text().strip()
            run("git", "clone", "--depth=1", "--no-checkout", "https://github.com/skosovsky/prompty.git",
                str(source), cwd=work, env=env)
            run("git", "fetch", "--depth=1", "origin", reference, cwd=source, env=env)
            run("git", "checkout", "--detach", "FETCH_HEAD", cwd=source, env=env)
        print("Current local source or audited CI snapshot:", subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=source, text=True).strip(), flush=True)
        run("go", "work", "init", str(checkout), str(source), cwd=work, env=env)
        local = env.copy()
        local["GOWORK"] = str(work / "go.work")
        run("go", "test", "-race", "-count=1", "-timeout=90s",
            "./...", cwd=checkout, env=local)


if __name__ == "__main__":
    main()
