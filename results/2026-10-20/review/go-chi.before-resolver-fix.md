# review_diff benchmark

Repo go-chi at 36611d2 (2026-10-20). 177 Go functions; loading the index took 0.4 s.

| Edit | Cases | Right | Precision | Recall | Median seconds |
|---|---|---|---|---|---|
| signature | 20 | 17 | 0.71 | 0.95 | 0.07 |
| remove | 20 | 16 | 0.88 | 0.98 | 0.07 |
| fs | 20 | 19 | 1.00 | 0.95 | 0.07 |
| network | 20 | 13 | 1.00 | 0.65 | 0.07 |
| db | 20 | 20 | 1.00 | 1.00 | 0.07 |
| control | 100 | 100 | 0.00 | 0.00 | 0.07 |
| branch | 20 | 20 | 1.00 | 1.00 | 0.07 |

Tests: 0 functions, 0 covered by go test (30 s).

| Hops | Says untested, really not covered | Says untested, but covered | Says tested, but not covered | Agreement |
|---|---|---|---|---|
| 1 | 0 | 0 | 0 | 0.00 |
| 3 | 0 | 0 | 0 | 0.00 |
| 6 | 0 | 0 | 0 | 0.00 |
| 10 | 0 | 0 | 0 | 0.00 |
| 99 | 0 | 0 | 0 | 0.00 |

| Commit | Files | Functions | break | behavior | test | size | Seconds |
|---|---|---|---|---|---|---|---|
| 36611d2 test(middleware): Improves test coverage (#700) | 7 | 9 | 0 | 5 | 0 | 2 | 0.3 |
| aba8618 fix: panic on Mount() when a router is mounted ... | 2 | 2 | 0 | 0 | 1 | 0 | 0.4 |
| 735ae2b Update compress middleware support Brotli suppo... | 1 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| 87c34a2 middleware: ContentCharset should skip requests... | 2 | 2 | 0 | 0 | 0 | 0 | 0.3 |
| 1576723 Add Clone method to Context (#1157) | 2 | 2 | 0 | 0 | 0 | 1 | 0.3 |
| 6adf733 Improve godoc: add links and titles (#1027) | 1 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| 3893906 fix(middleware): reject catch-all compress wild... | 2 | 2 | 0 | 0 | 0 | 0 | 0.3 |
| 9b6ddcd Don't duplicate methods in Allow: header for 40... | 2 | 2 | 0 | 0 | 0 | 1 | 0.3 |
| 29164f0 fix: don't drop handlers that collide with a Mo... | 2 | 5 | 0 | 0 | 2 | 3 | 0.3 |
| bc02284 docs: deployment recipe + verify checklist for ... | 4 | 2 | 0 | 0 | 0 | 0 | 0.3 |
| 60ecea5 feat(middleware): add text/markdown, text/csv, ... | 2 | 1 | 0 | 0 | 0 | 0 | 0.3 |
| 8b258c7 ci: pin GitHub Actions to full commit SHAs (#1116) | 1 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| caf87e6 feat(mux): support http QUERY method ietf rfc10... | 5 | 3 | 0 | 0 | 1 | 1 | 0.4 |
| 7fcb8a2 middleware: document printPrettyStack and harde... | 2 | 1 | 0 | 0 | 0 | 0 | 0.4 |
| 878fe71 Fix defaultLogEntry.Panic not respecting NoColo... | 3 | 6 | 0 | 1 | 4 | 0 | 0.3 |
| d7b767b feat(middleware): add text/xml and application/... | 2 | 1 | 0 | 0 | 0 | 0 | 0.3 |
| 3b50c7c Tidy build directives (#1113) | 2 | 0 | 0 | 0 | 0 | 0 | 0.3 |
| 2b9fca2 Honor Discard() in httpFancyWriter.ReadFrom (#1... | 2 | 3 | 0 | 0 | 0 | 0 | 0.3 |
| 3b17157 feat: middleware.ClientIP, a replacement for mi... | 7 | 34 | 0 | 4 | 2 | 7 | 0.3 |
| 818fdcf fix: set Request.Pattern from RoutePattern() (#... | 2 | 2 | 0 | 0 | 1 | 0 | 0.3 |
