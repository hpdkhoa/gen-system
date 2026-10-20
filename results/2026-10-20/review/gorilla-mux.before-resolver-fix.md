# review_diff benchmark

Repo gorilla-mux at db9d1d0 (2026-10-20). 107 Go functions; loading the index took 0.1 s.

| Edit | Cases | Right | Precision | Recall | Median seconds |
|---|---|---|---|---|---|
| signature | 20 | 18 | 0.91 | 0.97 | 0.07 |
| remove | 20 | 19 | 1.00 | 0.89 | 0.07 |
| fs | 20 | 20 | 1.00 | 1.00 | 0.07 |
| network | 20 | 19 | 1.00 | 0.95 | 0.07 |
| db | 20 | 20 | 1.00 | 1.00 | 0.07 |
| control | 100 | 100 | 0.00 | 0.00 | 0.07 |
| branch | 20 | 20 | 1.00 | 1.00 | 0.07 |

Tests: 0 functions, 0 covered by go test (3 s).

| Hops | Says untested, really not covered | Says untested, but covered | Says tested, but not covered | Agreement |
|---|---|---|---|---|
| 1 | 0 | 0 | 0 | 0.00 |
| 3 | 0 | 0 | 0 | 0.00 |
| 6 | 0 | 0 | 0 | 0.00 |
| 10 | 0 | 0 | 0 | 0.00 |
| 99 | 0 | 0 | 0 | 0.00 |

| Commit | Files | Functions | break | behavior | test | size | Seconds |
|---|---|---|---|---|---|---|---|
| db9d1d0 added test for router in context | 1 | 1 | 0 | 0 | 0 | 0 | 0.2 |
| fe14465 added conf method and defined initial test func | 2 | 2 | 0 | 0 | 1 | 0 | 0.3 |
| 1a848bf added simple initial implementation of router i... | 1 | 3 | 0 | 0 | 1 | 0 | 0.2 |
| 10f71ea added route metadata with tests | 3 | 6 | 0 | 0 | 1 | 0 | 0.2 |
| 525206d wrapper to getter | 1 | 3 | 0 | 0 | 2 | 1 | 0.2 |
| f184213 Updated comments and some consistency fixes | 2 | 3 | 0 | 0 | 2 | 1 | 0.2 |
| 0e6f2da Added route return to route.use method | 1 | 1 | 1 | 0 | 0 | 0 | 0.2 |
| 7cc5738 Added middleware support for individual routes | 3 | 7 | 0 | 0 | 2 | 2 | 0.2 |
| de7178d Optimize/new route regexp allocations (#732) | 2 | 2 | 0 | 0 | 0 | 0 | 0.2 |
| cfec64d chore: fix function name in comment | 1 | 0 | 0 | 0 | 0 | 0 | 0.1 |
| 7d9a6f7 Don't assign a variable for unused param | 1 | 1 | 1 | 0 | 0 | 0 | 0.1 |
| 374bd70 Add RegexpCompileFunc to override regexp.Compile | 3 | 5 | 0 | 0 | 0 | 0 | 0.2 |
| 976b536 Fixed processing of multiple queries for the sa... | 2 | 2 | 0 | 0 | 1 | 1 | 0.2 |
| 2b030fc Fix gorilla#739: Fixed handling of invalid quer... | 2 | 2 | 0 | 0 | 1 | 0 | 0.3 |
| e44017d perf: cut allocations (#691) | 5 | 23 | 0 | 1 | 3 | 2 | 0.3 |
| f79c3af Use "redirect" rather than "re-direct" in docs ... | 1 | 0 | 0 | 0 | 0 | 0 | 0.2 |
| b4617d0 update GitHub workflows (#734) | 5 | 0 | 0 | 0 | 0 | 0 | 0.1 |
| 3401478 fix SPA handler in README.md (#733) | 1 | 0 | 0 | 0 | 0 | 0 | 0.1 |
| 4a671cb Add GetVarNames() (#676) | 4 | 3 | 0 | 0 | 0 | 3 | 0.2 |
| 85123bf changed the routeVariables text content. (#708) | 1 | 0 | 0 | 0 | 0 | 0 | 0.2 |
