# review_diff benchmark

Repo gorilla-mux at db9d1d0 (2026-10-20). 107 Go functions; loading the index took 0.3 s.

| Edit | Cases | Right | Precision | Recall | Median seconds |
|---|---|---|---|---|---|
| signature | 20 | 19 | 1.00 | 0.99 | 0.07 |
| remove | 20 | 19 | 0.96 | 0.99 | 0.07 |
| fs | 20 | 20 | 1.00 | 1.00 | 0.07 |
| network | 19 | 19 | 1.00 | 1.00 | 0.07 |
| db | 20 | 20 | 1.00 | 1.00 | 0.07 |
| control | 100 | 100 | - | - | 0.07 |
| branch | 20 | 20 | 1.00 | 1.00 | 0.07 |

Tests: 107 functions, 100 covered by go test (3 s).

| Hops | Says untested, really not covered | Says untested, but covered | Says tested, but not covered | Agreement |
|---|---|---|---|---|
| 1 | 7 | 30 | 0 | 0.72 |
| 3 | 7 | 1 | 0 | 0.99 |
| 6 | 7 | 1 | 0 | 0.99 |
| 10 | 7 | 1 | 0 | 0.99 |
| 99 | 7 | 1 | 0 | 0.99 |

| Commit | Files | Functions | break | behavior | test | size | Seconds |
|---|---|---|---|---|---|---|---|
| db9d1d0 added test for router in context | 1 | 1 | 0 | 0 | 0 | 0 | 0.4 |
| fe14465 added conf method and defined initial test func | 2 | 2 | 0 | 0 | 1 | 0 | 0.5 |
| 1a848bf added simple initial implementation of router i... | 1 | 3 | 0 | 0 | 1 | 0 | 0.4 |
| 10f71ea added route metadata with tests | 3 | 6 | 0 | 0 | 1 | 0 | 0.4 |
| 525206d wrapper to getter | 1 | 3 | 0 | 0 | 0 | 1 | 0.4 |
| f184213 Updated comments and some consistency fixes | 2 | 3 | 0 | 0 | 0 | 1 | 0.4 |
| 0e6f2da Added route return to route.use method | 1 | 1 | 1 | 0 | 0 | 0 | 0.4 |
| 7cc5738 Added middleware support for individual routes | 3 | 7 | 0 | 0 | 0 | 2 | 0.4 |
| de7178d Optimize/new route regexp allocations (#732) | 2 | 2 | 0 | 0 | 0 | 0 | 0.4 |
| cfec64d chore: fix function name in comment | 1 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| 7d9a6f7 Don't assign a variable for unused param | 1 | 1 | 1 | 0 | 0 | 0 | 0.3 |
| 374bd70 Add RegexpCompileFunc to override regexp.Compile | 3 | 5 | 0 | 0 | 0 | 0 | 0.3 |
| 976b536 Fixed processing of multiple queries for the sa... | 2 | 2 | 0 | 0 | 0 | 1 | 0.4 |
| 2b030fc Fix gorilla#739: Fixed handling of invalid quer... | 2 | 2 | 0 | 0 | 0 | 0 | 0.4 |
| e44017d perf: cut allocations (#691) | 5 | 23 | 0 | 1 | 0 | 2 | 0.5 |
| f79c3af Use "redirect" rather than "re-direct" in docs ... | 1 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| b4617d0 update GitHub workflows (#734) | 5 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| 3401478 fix SPA handler in README.md (#733) | 1 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| 4a671cb Add GetVarNames() (#676) | 4 | 3 | 0 | 0 | 0 | 3 | 0.4 |
| 85123bf changed the routeVariables text content. (#708) | 1 | 0 | 0 | 0 | 0 | 0 | 0.3 |
