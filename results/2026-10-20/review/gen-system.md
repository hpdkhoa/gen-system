# review_diff benchmark

Repo gen-system at 8d2ac3c (2026-10-20). 1473 Go functions; loading the index took 19.0 s.

| Edit | Cases | Right | Precision | Recall | Median seconds |
|---|---|---|---|---|---|
| signature | 20 | 20 | 1.00 | 1.00 | 0.11 |
| remove | 20 | 20 | 1.00 | 1.00 | 0.11 |
| fs | 17 | 17 | 1.00 | 1.00 | 0.10 |
| network | 19 | 19 | 1.00 | 1.00 | 0.10 |
| db | 20 | 20 | 1.00 | 1.00 | 0.11 |
| control | 100 | 100 | - | - | 0.10 |
| branch | 20 | 20 | 1.00 | 1.00 | 0.10 |

Tests: 1473 functions, 1054 covered by go test (266 s).

| Hops | Says untested, really not covered | Says untested, but covered | Says tested, but not covered | Agreement |
|---|---|---|---|---|
| 1 | 401 | 707 | 18 | 0.51 |
| 3 | 349 | 210 | 70 | 0.81 |
| 6 | 266 | 81 | 153 | 0.84 |
| 10 | 244 | 66 | 175 | 0.84 |
| 99 | 244 | 66 | 175 | 0.84 |

| Commit | Files | Functions | break | behavior | test | size | Seconds |
|---|---|---|---|---|---|---|---|
| 8d2ac3c Python comprehensions are no loops; environment... | 325 | 5 | 0 | 0 | 0 | 4 | 16.8 |
| 4482d63 Python methods as one symbol; wrong links and l... | 578 | 18 | 0 | 0 | 1 | 12 | 18.5 |
| 8801420 Studio CFG by dagre; source files up to 512 KB;... | 2243 | 2 | 0 | 0 | 0 | 1 | 34.2 |
| c9ab722 README: documentation, contributing and securit... | 1 | 0 | 0 | 0 | 0 | 0 | 14.4 |
| 65d2b86 README: a short man page; the measurements move... | 2 | 0 | 0 | 0 | 0 | 0 | 9.3 |
| b3897a1 Results: gen-system agrees with cobc on every C... | 11 | 0 | 0 | 0 | 0 | 0 | 9.1 |
| a815e0c bench/compare: the dirty check ignores line end... | 1 | 0 | 0 | 0 | 0 | 0 | 9.1 |
| 36c3564 bench/compare: diff gen-system's COBOL call edg... | 9 | 21 | 0 | 10 | 13 | 15 | 9.8 |
| 7ed6caa COBOL: decide fixed or free format per file; su... | 4 | 7 | 0 | 0 | 0 | 5 | 8.6 |
| 7db7306 README, landing page and demo: the corrected pa... | 680 | 0 | 0 | 0 | 0 | 0 | 14.5 |
| 257d4a1 CI: gofmt, vet and tests, plus the exact CobolC... | 1 | 0 | 0 | 0 | 0 | 0 | 10.7 |
| 62eff66 COBOL: read every program in a file; PERFORM st... | 16 | 30 | 0 | 0 | 3 | 11 | 9.2 |
| b0a860b Calls in else branches become call edges | 3 | 4 | 0 | 0 | 0 | 3 | 8.9 |
| 5577eab Drop the compiler accuracy claim; add a code of... | 8 | 1 | 0 | 0 | 1 | 0 | 8.8 |
| 484e488 Add CONTRIBUTING.md | 2 | 0 | 0 | 0 | 0 | 0 | 8.7 |
| 9b59028 Add demo GIF | 2 | 0 | 0 | 0 | 0 | 0 | 8.5 |
| 0188a8f Fix the last Studio bugs: Go init order, field ... | 148 | 9 | 0 | 0 | 5 | 3 | 9.2 |
| e549554 site/build.py: run the snapshot from its temp f... | 1 | 1 | 0 | 0 | 1 | 0 | 9.9 |
| 32a3ef8 Prepare for launch: stdlib calls resolve, handl... | 15 | 9 | 0 | 0 | 2 | 4 | 9.0 |
| 1d4e300 README: demo badge and link to the live demo | 1 | 0 | 0 | 0 | 0 | 0 | 8.4 |
