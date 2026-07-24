# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/).

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
