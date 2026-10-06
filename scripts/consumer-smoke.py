#!/usr/bin/env python3
"""Build current modules as unpublished release-layout archives, without go.work/replace."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import zipfile

root = Path(__file__).resolve().parents[1]
work = Path(tempfile.mkdtemp(prefix="routery-consumer-", dir="/tmp"))
proxy = work / "proxy"
version = "v0.6.0-task13"
modules = [root] + sorted((root / "ext").glob("*/go.mod"))
modules = [p.parent if p.name == "go.mod" else p for p in modules]
paths = []
for directory in modules:
    original = (directory / "go.mod").read_text()
    module = re.search(r"^module (\S+)", original, re.M).group(1)
    paths.append(module)
    mod = re.sub(r"(github\.com/skosovsky/routery(?:/ext/\w+)?) v0\.0\.0", rf"\1 {version}", original)
    mod = re.sub(r"^replace github\.com/skosovsky/routery.*\n", "", mod, flags=re.M)
    destination = proxy / module / "@v"
    destination.mkdir(parents=True)
    (destination / f"{version}.mod").write_text(mod)
    (destination / f"{version}.info").write_text(json.dumps({"Version": version, "Time": "2026-10-06T00:00:00Z"}))
    (destination / "list").write_text(version + "\n")
    with zipfile.ZipFile(destination / f"{version}.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for item in directory.rglob("*"):
            if not item.is_file():
                continue
            relative = item.relative_to(directory)
            if any(part.startswith(".") or part in {"vendor", "__pycache__"} for part in relative.parts):
                continue
            if any((directory / Path(*relative.parts[:i]) / "go.mod").exists() for i in range(1, len(relative.parts))):
                continue
            if item.name in {"go.work", "go.work.sum"}:
                continue
            if item.suffix not in {".go", ".mod", ".sum", ".md"} and item.name != "LICENSE":
                continue
            data = mod.encode() if relative == Path("go.mod") else item.read_bytes()
            archive.writestr(f"{module}@{version}/{relative.as_posix()}", data)
consumer = work / "consumer"
consumer.mkdir()
(consumer / "go.mod").write_text("module consumer.example/task13\n\ngo 1.27.1\n\nrequire (\n" + "".join(f"\t{module} {version}\n" for module in paths) + ")\n")
(consumer / "main.go").write_text('''package main
import (
 "context"
 "github.com/skosovsky/routery"
 redis "github.com/skosovsky/routery/ext/redis"
 _ "github.com/skosovsky/routery/ext/http"
 _ "github.com/skosovsky/routery/ext/grpc"
 _ "github.com/skosovsky/routery/ext/sql"
 _ "github.com/skosovsky/routery/ext/mongo"
 _ "github.com/skosovsky/routery/ext/kafka"
 _ "github.com/skosovsky/routery/ext/s3"
 _ "github.com/skosovsky/routery/ext/otel"
)
var _ = redis.NewStringRouteHandler[int]
func main() {
 table := routery.NewBasicRouteTable[int,string]()
 table.Route("leaf",0,nil,func(routery.RouteCall[int])(routery.BasicRouteResult[string],error){return routery.BasicHandled("ok"),nil})
 compiled,err:=table.Build();if err!=nil{panic(err)}
 result,err:=compiled.Dispatch(context.Background(),0);if err!=nil||result.Payload!="ok"{panic("dispatch failed")}
}
''')
env = dict(os.environ, GOWORK="off", GOPRIVATE="", GONOPROXY="none", GOPROXY=proxy.as_uri()+",https://proxy.golang.org", GONOSUMDB="github.com/skosovsky/routery*", GOMODCACHE=str(work / "modcache"), GOPATH=str(work / "gopath"), GOCACHE=os.environ.get("GOCACHE", "/tmp/routery-review-gocache"))
print("Consumer workspace:", work, flush=True)
for args in (["go", "mod", "tidy"], ["go", "list", "-m", "-json", "all"], ["go", "build", "./..."], ["go", "run", "."]):
    subprocess.run(args, cwd=consumer, env=env, check=True)
assert "replace" not in (consumer / "go.mod").read_text()
print("PASS: all nine current modules consumed as release-layout ZIPs with GOWORK=off, no local replace", flush=True)
