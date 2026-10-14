## What this changes

<!-- One or two sentences. Link the issue: Fixes #123 -->

## Why

<!-- The wrong read, the bug or the need. For a parser fix, paste the smallest source that failed. -->

## How I checked it

- [ ] `gofmt -l .` in `generator/` prints nothing
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] I added a test that fails without this change
- [ ] Parser, symbol or control flow change: `freeze-cobol-baseline.sh verify` passes,
      or I list below which number moved and why
- [ ] Studio change: I edited both copies of `studio-v23.html` and rebuilt the demo with
      `python site/build.py`
- [ ] No new dependency, or an issue approved it first

## Numbers that moved

<!-- Baseline counts before and after, or "none". -->

## Notes for review

<!-- Anything I should read first, open questions, or follow-up work. -->
