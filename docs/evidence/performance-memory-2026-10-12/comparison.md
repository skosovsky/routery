| Scenario | Before median ns | After median ns | Change | Before B/allocs | After B/allocs | Gate |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| BenchmarkAuditMissDispatch/1 | 159.4 | 138.2 | -13.30% | 24/2 | 8/1 | PASS |
| BenchmarkAuditMissDispatch/1024 | 72053 | 54987 | -23.69% | 16392/1025 | 8/1 | PASS |
| BenchmarkAuditMissDispatch/64 | 4763 | 3543 | -25.61% | 1032/65 | 8/1 | PASS |
| BenchmarkPerformanceDispatch/error | 344.3 | 352.4 | +2.35% | 408/5 | 408/5 | PASS |
| BenchmarkPerformanceDispatch/last | 4748 | 3760 | -20.81% | 1048/66 | 24/2 | PASS |
| BenchmarkPerformanceDispatch/mixed | 481.8 | 423.7 | -12.06% | 72/5 | 24/2 | PASS |
| BenchmarkPerformanceDispatch/nested | 453.3 | 448.8 | -0.99% | 88/4 | 56/3 | PASS |
| BenchmarkPerformanceDispatch/sink | 391 | 372.9 | -4.63% | 72/5 | 56/4 | PASS |
