## What does this change and why?

<!-- One or two sentences. Link an issue if there is one. -->

## Checklist

- [ ] `gofmt -l .` is clean
- [ ] `go vet ./...` is clean
- [ ] `go test ./...` passes
- [ ] New behavior has a test that actually exercises it end-to-end
      (through the real command/function path, not only a unit in
      isolation) — this repo has a history of "unit-tested but
      unreachable" bugs, see CHANGELOG.md
- [ ] If this fixes a bug: the added test was confirmed to fail against
      the old code before the fix (mutation-tested), not just written
      against the new behavior
- [ ] `README.md` / `CHANGELOG.md` updated if this changes user-visible behavior

## Anything the reviewer should know?

<!-- Design tradeoffs, things deliberately left out of scope, etc. -->
