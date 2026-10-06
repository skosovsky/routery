#!/usr/bin/env python3
"""Exercise fuzz discovery, exact selectors and failure propagation with a fake Go runner."""
import os
from pathlib import Path
import subprocess
import tempfile

script = Path(__file__).resolve().with_name("fuzz.sh")
work = Path(tempfile.mkdtemp(prefix="routery-fuzz-test-", dir="/tmp"))
runner = work / "go"
runner.write_text('''#!/usr/bin/env python3
import os, sys
from pathlib import Path
args=sys.argv[1:]; mode=os.environ['MODE']
with Path(os.environ['CALLS']).open('a') as log: log.write(' '.join(args)+'\\n')
if args[0]=='list':
 if mode=='list-fail': sys.exit(7)
 print('example/pkg'); sys.exit(0)
if '-list' in args:
 if mode=='build-fail': sys.exit(8)
 if mode=='none': print('ok example/pkg'); sys.exit(0)
 print('FuzzOne')
 if mode=='multi': print('FuzzTwo')
 sys.exit(0)
if mode=='target-fail': sys.exit(9)
''')
runner.chmod(0o700)
for mode, expected in {"none":0,"one":0,"multi":0,"list-fail":7,"build-fail":8,"target-fail":9}.items():
    calls = work / (mode+".log")
    result = subprocess.run(["sh", str(script)], env=dict(os.environ, GO=str(runner), MODE=mode, CALLS=str(calls), FUZZTIME="1s"), capture_output=True, text=True)
    assert result.returncode == expected, (mode,result.returncode,result.stderr)
    commands = calls.read_text()
    assert "-fuzz ." not in commands
    if mode == "multi":
        assert "-fuzz ^FuzzOne$" in commands and "-fuzz ^FuzzTwo$" in commands
    if mode == "none":
        assert "-fuzz " not in commands
    print("PASS:", mode)
