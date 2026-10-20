# review_diff benchmark

Repo spf13-cobra at adbc881 (2026-10-20). 264 Go functions; loading the index took 0.5 s.

| Edit | Cases | Right | Precision | Recall | Median seconds |
|---|---|---|---|---|---|
| signature | 20 | 19 | 0.98 | 0.98 | 0.16 |
| remove | 20 | 18 | 0.96 | 0.98 | 0.16 |
| fs | 19 | 19 | 1.00 | 1.00 | 0.16 |
| network | 20 | 20 | 1.00 | 1.00 | 0.16 |
| db | 20 | 20 | 1.00 | 1.00 | 0.16 |
| control | 100 | 100 | - | - | 0.16 |
| branch | 20 | 20 | 1.00 | 1.00 | 0.16 |

Tests: 264 functions, 232 covered by go test (10 s).

| Hops | Says untested, really not covered | Says untested, but covered | Says tested, but not covered | Agreement |
|---|---|---|---|---|
| 1 | 31 | 129 | 1 | 0.51 |
| 3 | 29 | 37 | 3 | 0.85 |
| 6 | 29 | 18 | 3 | 0.92 |
| 10 | 29 | 18 | 3 | 0.92 |
| 99 | 29 | 18 | 3 | 0.92 |

| Commit | Files | Functions | break | behavior | test | size | Seconds |
|---|---|---|---|---|---|---|---|
| adbc881 fix: resolve macOS test link failure and update... | 3 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| ad460ea Add cobra unique args validator (#2397) | 3 | 6 | 0 | 0 | 1 | 2 | 0.5 |
| 746ef07 fix: prevent completions from mutating os.Args ... | 2 | 2 | 0 | 0 | 0 | 1 | 0.5 |
| f2878ba Ignore the linter error (#2390) | 1 | 1 | 0 | 0 | 1 | 0 | 0.5 |
| 85c12c2 projects_using_cobra: add Ollama (#2389) | 1 | 0 | 0 | 0 | 0 | 0 | 0.5 |
| 61968e8 fix: quote args in fish shell completion (#2285) | 1 | 1 | 0 | 0 | 0 | 0 | 0.5 |
| 10d4b48 perf: Loop over the bash variable directly inst... | 1 | 1 | 0 | 0 | 0 | 0 | 0.5 |
| 88b30ab chore: Migrate from gopkg.in/yaml.v3 to go.yaml... | 3 | 0 | 0 | 0 | 0 | 0 | 0.5 |
| 346d408 fix: actions/setup-go v6 (#2337) | 1 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| fc81d20 refactor: change minUsagePadding from var to co... | 1 | 0 | 0 | 0 | 0 | 0 | 0.5 |
| 117698a refactor: replace several vars with consts (#2328) | 1 | 0 | 0 | 0 | 0 | 0 | 0.6 |
| e2dd29d Add documentation for repeated flags functional... | 1 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| 0629892 Fix linter (#2327) | 1 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| 7da941c chore: Bump pflag to v1.0.9 (#2305) | 2 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| 51d6751 Bump pflag to 1.0.8 (#2303) | 3 | 1 | 0 | 0 | 0 | 0 | 0.5 |
| 3f3b818 Update README.md with new logo | 1 | 0 | 0 | 0 | 0 | 0 | 0.5 |
| dcaf42e Add Periscope to the list of projects using Cob... | 1 | 0 | 0 | 0 | 0 | 0 | 0.4 |
| 6dec1ae The default ShellCompDirective can be customize... | 3 | 3 | 0 | 0 | 0 | 1 | 0.5 |
| c8289c1 chore(golangci-lint): add some exclusion presets | 1 | 0 | 0 | 0 | 0 | 0 | 0.5 |
| 4af7b64 refactor: apply golangci-lint autofixes, work a... | 6 | 7 | 0 | 0 | 0 | 0 | 0.6 |
