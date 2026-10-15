# gen-system against cobc

178 files compared; 0 rejected by cobc and left out.

| | cobc | gen-system | both | only cobc | only gen-system |
|---|---|---|---|---|---|
| CALL and PERFORM sites in the file | 3054 | 3054 | 3054 (100.0% of cobc) | 0 | 0 |
| sites from copybooks, by file and target | 1078 | 1078 | 1078 | 0 | 0 |

Callers: on 3054 matched sites, both name the same program and paragraph on 3054 (100.0%).

Resolution on matched sites: call_outside_ok 83, call_to_program_ok 2514, dynamic 34, perform_ok 423.

Left out of gen-system: 407 FUNCTION; 0 edges matched no CALL, PERFORM, FUNCTION or COPY on their line.
