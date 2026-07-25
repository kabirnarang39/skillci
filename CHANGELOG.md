# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [v0.3.1] — 2026-07-25

### Added
- `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1).
- Go Report Card and pkg.go.dev badges in README.
- GitHub social-preview image (`.github/assets/social-preview.png`).

### Fixed
- Contact email in `SECURITY.md` and a README example pointed at a
  work address; corrected to the maintainer's personal email.
- Removed a `star-history.com` badge that was failing server-side
  (500/timeout) rather than rendering.

## [v0.3.0] — 2026-07-25

### Added
- `check`'s `judge`/`judge_strict` assertion now reasons step-by-step
  before each verdict (the G-Eval technique promptfoo's `llm-rubric` and
  deepeval's `GEval` are built on), supports `judge_mode: batched |
  isolated` (global `.skillci.yaml` default, per-case
  `assert.judge_mode` override) to trade cost for full per-criterion
  reasoning isolation, caches verdicts to `.skillci/judge-cache.json` so
  a repeat call against an unchanged response never re-pays for the API
  call, and supports `judge_samples` self-consistency voting — the same
  asymmetry `flake_retries` already closed for the case model, now
  closed for the judge model's own sampling variance too.

### Fixed
- `check --verify-pinned-sources` no longer fetches loopback, private,
  link-local, unspecified, or multicast addresses — a skill's own
  `pinned_sources` frontmatter was previously fetched with no host
  restriction, so a malicious skill could point it at an internal service
  (e.g. a cloud metadata endpoint) and turn a hash check into SSRF
  (CWE-918). The guard validates the resolved IP at actual dial time (for
  the initial request and any redirect), closing the DNS-rebinding gap a
  separate check-then-dial step would leave open.
- `skillci bisect`: a run killed before its deferred cleanup could run
  (SIGKILL, or an unhandled SIGINT/SIGTERM from a CI job timeout) used to
  leak its temporary `git worktree` — both the checked-out directory and
  git's own worktree registration — forever, since a directory that still
  exists on disk is invisible to `git worktree prune`. Every `bisect` run
  now sweeps and removes any orphaned worktree from a prior interrupted
  run before starting.
- `regress --open-pr`: the generated-eval-case commit is now scoped to the
  generated file(s) instead of running a bare `git commit`, which commits
  the entire index — any unrelated content already staged in the working
  tree before `--open-pr` ran (e.g. a developer's own in-progress work)
  would otherwise be swept into the commit, pushed to a throwaway branch,
  and included in the pull request opened on GitHub.
- `check`: every line number reported for a `SKILL.md` issue (AST01/02/03/05
  findings, committed-secret detection, duplicate-line bloat warnings, and
  a referenced file's missing/traversal/case-mismatch/portability issues)
  was computed relative to the body text *after* the frontmatter block,
  not the real file — so it always pointed several lines above the actual
  offending line, by however many lines the frontmatter itself occupied.
  Caught live via the new VS Code extension's Problems panel, which
  visibly anchored diagnostics on the wrong line; the CLI's own
  `file:line:` text output had the identical bug.
- A `snapshot`-enabled case that drifted, then later reverted back to
  matching its golden baseline, left the earlier run's pending snapshot
  change sitting on disk indefinitely. Left in place, `accept --model`
  had no way to know it was stale and would promote it as the new golden
  baseline even though it no longer reflected what the case actually
  produces. A case going back to matching golden now clears any pending
  change from an earlier drifted run.
- `fuzz`'s synonym-swap operator only ever generated a mutation for the
  *first* trigger-relevant word it found in a prompt — every other
  eligible word (e.g. "fix" in "review and fix this code", with "review"
  matched first) was silently never fuzz-tested at all, for the lifetime
  of that eval case. Now generates one mutation per eligible word, each
  swapping only that word, so a flipped trigger can still be attributed
  to a specific swap.
- `.skillci.yaml`'s `fail_on` now rejects an unrecognized value at load
  time instead of silently falling back to the `regression` policy.
  Previously a typo (e.g. `any_fial`) or an otherwise invalid value
  produced no error — `ShouldFailCI`'s switch defaults to `regression`
  for anything it doesn't recognize — so a user who configured what they
  thought was the strictest policy could end up with the loosest one and
  never find out.
- `evals/*.yaml` case loading now rejects a missing `name:` or a name
  duplicated across two different files. Previously undetected — every
  feature that looks a case up by name (bisect's target selection,
  regress's per-(case,model) history tracking, accept) treats the name as
  a unique key, so a duplicate (an easy mistake: copy an existing case
  file to start a new one, forget to rename it) silently made that lookup
  ambiguous instead of failing loudly at load time.
- Dashboard ingest token comparison now runs in constant time
  (`crypto/subtle.ConstantTimeCompare`) instead of a plain `==`, which
  short-circuits on the first mismatched byte and turns request latency
  into a side channel an attacker could use to recover a valid token
  byte-by-byte rather than having to guess it whole.
- VS Code extension: `skillci.lintOnType` re-linted on every debounced
  edit as advertised, but skillci always reads `SKILL.md` from disk —
  an unsaved keystroke was invisible to it until the file was actually
  saved, so "live linting as you type" silently only ever reflected
  whatever was last saved. Each debounced edit now mirrors the live
  buffer into a scratch temp directory (with the skill's other files
  symlinked in so referenced-file checks still resolve) and lints that,
  remapping diagnostics back onto the real files. Caught while directly
  answering whether the extension had actually been tested thoroughly —
  it hadn't, for this specific path.
- The Anthropic API client now retries a transient failure (429 rate
  limit, 500/502/503, or Anthropic's 529 "overloaded") up to 3 times with
  backoff instead of failing immediately. Previously a single flaky
  response failed the entire call — and, since `regress`'s matrix loop
  aborts the whole run on the first error, wasted every already-completed
  API call in that run, not just the one that hit it. A genuine client
  error (e.g. 401 invalid key) still fails on the first attempt, since
  retrying it can never succeed.
- `skillci-server`'s `SKILLCI_INGEST_TOKENS` parsing now rejects an entry
  with an empty token (e.g. `=owner/repo`) instead of silently accepting
  it. Previously undetected — a realistic misconfiguration (a templated
  env var like `${SECRET}=owner/repo` where `$SECRET` is accidentally
  unset at deploy time) registered an empty-string token as a valid
  scope, and a request sent with the literal header `Authorization:
  Bearer ` (empty bearer value) would match it — a real authentication
  bypass, not just a useless config entry.
- `regress`, `eval`, `fuzz`, `bisect`, `accept`, `badge`, `diff`, and
  `init` now silence Cobra's default "print the flags/usage block on any
  RunE error" behavior, matching the precedent `check` already set (a
  lint failure isn't user error). Caught via live end-to-end testing
  with a local stub server: a normal `regress` failure — the single most
  common outcome the tool exists to report in CI — was dumping a full
  wall of flag help on every failing build.
- Every command was printing its own error message twice: Cobra's default
  behavior prints `Error: <err>` itself on any RunE/parse error, and
  main() separately printed the same error again before exiting. Caught
  via live testing — a real `check` run against a mismatched pinned
  source showed both `Error: 1 lint issue(s) found` and a bare repeat of
  the same text. Root command now sets `SilenceErrors: true`, leaving
  main()'s own print as the single source of truth.
- Generated eval case files (`evals/_generated/*.yaml`, written by the
  self-growing eval loop) spelled out every `Assertions` field the case
  never set as a literal `null` or `[]` — around 15 lines of noise for a
  typical case that only sets `triggered`. Caught live: `accept`-ing a
  real generated case showed the bloated file. `evalspec.Assertions`'
  YAML tags now carry `omitempty`, so a generated case reads as cleanly
  as a hand-written one. Read behavior (parsing an existing
  `evals/*.yaml`) is unaffected — `omitempty` only changes marshaling.
- A case's own `assert.judge_mode` override had no validation, unlike the
  global `judge_mode` in `.skillci.yaml` (rejected at load time). A typo
  like `judge_mode: isloated` on a case silently fell through to
  `"batched"` instead of failing loud. Now rejected with a clear error
  naming the case and the invalid value.
- `judgecache.Cache.Samples` returned its internal `[]Sample` slice
  directly — a caller mutating the result (or one of its elements) could
  corrupt the cache's own stored data. Now returns a copy.

### Changed
- The GitHub Action's `version` input now defaults to `v0.3.0`, and the
  README's own examples (`go install`, the Action reference, the
  `version` input) are bumped to match.
- README: added a "How this compares" section (an honest, sourced
  comparison against skillgrade, AgentLinter, and promptfoo/deepeval/
  braintrust) and expanded "What's inside" to surface the VS Code
  extension and hosted dashboard, both previously undocumented above the
  fold.

## [v0.2.0] — 2026-07-24

### Added
- `skillci bisect`: automatic merge-commit detection, falling back from
  binary search to a full linear scan of the candidate range so a merge
  commit itself is never (incorrectly) reported as the culprit; warns if
  the non-linear history has more than one commit where behavior
  transitions from passing to failing.
- `skillci bisect`: verified (case, model, commit) results now persist to
  `.skillci/bisect-cache.json`, so a repeat bisect run — after an
  interruption, or investigating a related case whose range overlaps
  commits already tested — never re-tests a commit it already has an
  answer for.
- `skillci regress --auto-bisect`: runs bisect automatically on a newly
  detected regression instead of only printing the suggested command.
- `skillci regress --open-pr`: commits a self-growing-loop-generated eval
  case onto a new branch, pushes it, and opens a pull request, instead of
  leaving a file under `evals/_generated/` for someone to notice manually.
- Generated eval cases now carry the failure context that produced them
  (model, detection time, the model's actual response) as a YAML comment
  header, not just name/prompt/assert.
- `check`: AST05 (untrusted external instructions) and AST02 (unpinned
  dependency — an install/pull command or Dockerfile `FROM` pinned to a
  floating `latest` tag) security lint rules, extending OWASP Agentic
  Skills Top 10 coverage from 4 to 6 of 10 categories.
- `check`: four new lint rules catching a `*_strict: true` set without its
  base assertion also enabled (`snapshot_strict`, `latency_strict`,
  `flake_strict`, `judge_strict`), where the strict flag would otherwise
  silently have no effect.
- Dashboard ingest tokens can now be scoped to a specific owner/repo
  (`SKILLCI_INGEST_TOKENS`), alongside the existing single shared-token
  mode (`SKILLCI_INGEST_TOKEN`), so a leaked token can't forge results for
  a different project on a shared dashboard instance.
- Packaging: prebuilt binaries (linux/darwin, amd64/arm64) via GoReleaser,
  published to GitHub Releases and a Homebrew tap
  (`kabirnarang39/homebrew-skillci`), alongside the existing `go install`.
- GitHub Action: `pr-comment` input posts regress results directly on the
  triggering pull request (updating the same comment on repeat pushes
  instead of spamming a new one each time).

### Fixed
- `.skillci/history.json` is now capped to the 200 most recent runs
  (matching the dashboard's existing retention convention) instead of
  growing unbounded in the git-committed state file.
- Dashboard ingest request bodies are capped at 1MB before JSON decoding.

### Changed
- The GitHub Action's `version` input now defaults to `v0.2.0` (a real
  pinned release), and its own example in the README no longer floats on
  `@latest`.

## [v0.1.0] — 2026-07-22

First tagged release. Full feature set at this tag:

- `skillci check`: OWASP Agentic Skills Top 10 security scan (4
  categories) + skill-bloat lint, no API calls.
- `skillci eval` / `skillci regress`: cross-model eval matrix with a
  self-growing eval loop — an uncovered failure proposes a generated eval
  case instead of only failing.
- Opt-in, non-strict-by-default assertion mechanisms: `snapshot` /
  `snapshot_strict`, `fuzz` / `fuzz_strict`, `flake_retries` /
  `flake_strict`, `judge` / `judge_model` / `judge_strict`.
- Cost and latency budget assertions (`max_output_tokens`,
  `max_latency_ms`, `max_cost_usd`).
- `dimensions` / `strict_dimensions` for slice-level CI gating.
- `skillci bisect`: git-worktree-based binary search over a skill's own
  commit history to find which commit broke an eval case.
- Optional self-hosted dashboard (Postgres-backed).
- GitHub Actions integration.
