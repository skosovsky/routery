"""Compare the fixed benchmark gates using raw Go output, no dependencies."""
import re
import statistics
import sys

PATTERN = re.compile(r'^(Benchmark(?:AuditMissDispatch|PerformanceDispatch)/\S+)-\d+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op$', re.M)
EXPECTED = {f'BenchmarkAuditMissDispatch/{n}' for n in (1, 64, 1024)} | {f'BenchmarkPerformanceDispatch/{s}' for s in ('last', 'mixed', 'nested', 'error', 'sink')}

def samples(path):
    text = open(path).read()
    if '\nPASS\n' not in text or 'AUDIT_LOCK_RELEASED 0' not in text:
        raise AssertionError(f'incomplete benchmark log: {path}')
    result = {}
    for name, ns, size, allocs in PATTERN.findall(text):
        result.setdefault(name, []).append((float(ns), int(size), int(allocs)))
    if set(result) != EXPECTED:
        raise AssertionError(f'missing/unexpected scenarios in {path}: {set(result) ^ EXPECTED}')
    if any(len(rows) < 3 for rows in result.values()):
        raise AssertionError(f'fewer than 3 samples in {path}')
    return result

before, after = map(samples, sys.argv[1:3])
print('| Scenario | Before median ns | After median ns | Change | Before B/allocs | After B/allocs | Gate |')
print('| --- | ---: | ---: | ---: | ---: | ---: | --- |')
failures = []
for name in sorted(EXPECTED):
    old, new = before[name], after[name]
    old_ns, new_ns = (statistics.median(row[0] for row in rows) for rows in (old, new))
    # Every allocation/byte sample must satisfy its gate, not merely the median.
    old_size, old_alloc = min(row[1] for row in old), min(row[2] for row in old)
    new_size, new_alloc = max(row[1] for row in new), max(row[2] for row in new)
    passed = new_ns <= 1.1 * old_ns and new_size <= old_size and new_alloc <= old_alloc
    if name.startswith('BenchmarkAuditMissDispatch/'):
        passed = passed and new_size <= 8 and new_alloc <= 1
    if not passed:
        failures.append(name)
    print(f'| {name} | {old_ns:g} | {new_ns:g} | {(new_ns / old_ns - 1) * 100:+.2f}% | {old_size}/{old_alloc} | {new_size}/{new_alloc} | {"PASS" if passed else "FAIL"} |')
if failures:
    raise SystemExit(f'failed gates: {failures}')
