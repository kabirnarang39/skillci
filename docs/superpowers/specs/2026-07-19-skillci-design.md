# SkillCI — Design

Status: approved (brainstorming), pending implementation plan
Date: 2026-07-19

## 1. Problem

Claude Skills (`SKILL.md` + progressive-disclosure format, spec published Dec 2025 at
agentskills.io, cross-agent adoption across 26+ tools within two months) launched Oct 16,
2025. Nine months in, tooling around it is fragmented, not absent: lint, eval, token-budget
scoring, and security scanning each have 2-8 small competing tools, none dominant, several
already abandoned (claudelint → renamed skillsaw). No project plays the ESLint/Biome/Vitest
role — the single, opinionated, one-command consolidator.

The sharpest, least-covered sub-gap: **cross-model regression tracking**. Anthropic's own
docs acknowledge model upgrades can silently change skill behavior and recommend manual
"shadow-testing." Only one indie tool has any model-variance scoring, and it's a one-shot
local scorer, not a continuous, historical, dashboarded signal.

Ruled out explicitly (research-backed, see prior session): spec-driven-dev tooling (owned by
github/spec-kit, 111k stars), JIRA→PR automation (owned by official Atlassian/Anthropic app),
skill marketplace/registry (23.4k skills already indexed, a 5.2k-star curated list exists,
zero technical moat in pure aggregation).

## 2. Goal

Ship `skillci`: a Go CLI that lints, evals, and regression-tests Claude Skills against a
matrix of Claude models, wired into CI via a GitHub Action, backed by an opt-in hosted
dashboard that tracks pass/fail history over time and gives skills a public,
shareable/screenshot-able trust signal — the caniuse.com of Claude Skills compatibility.

Target: an ongoing, maintained open source project (not a weekend ship), with a moat built
from accumulated historical regression data rather than the CLI code itself.

## 3. Architecture

Three components:

1. **`skillci` CLI** (Go, single static binary — no runtime dependency, brew/curl installable)
2. **GitHub Action** (thin wrapper around the binary) — runs on PR/push, gates CI on
   regressions, posts PR comments with diffs, commits the updated badge
3. **Dashboard** (hosted web app + ingestion API, Postgres-backed time series) — opt-in,
   public per-skill pages + a compatibility leaderboard

Data flow: author writes `evals/*.yaml` in their skill folder → CLI runs the model matrix →
results land in `.skillci/history.json` + a regenerated SVG badge → optionally POSTed to the
dashboard ingestion API → dashboard stores time series, renders the public page.

## 4. Eval suite format

Lives alongside the skill, e.g. `evals/*.yaml`. Aligned with Anthropic's own skill-creator
train/test-prompt convention to minimize adoption friction rather than inventing a new format.

```yaml
name: "triggers-on-pr-review-request"
prompt: "Can you review this PR for SOLID violations?"
skill_under_test: "pr-review"
assert:
  triggered: true
  contains: ["SOLID", "verdict"]
  not_contains: ["I cannot"]
  max_tokens_loaded: 3000
```

Three assertion classes at MVP:
- `triggered` — did the skill's description actually cause it to fire (the single most
  common real-world complaint per research)
- `contains` / `not_contains` — content assertions
- `max_tokens_loaded` — progressive-disclosure token-budget gate (folds in the
  token-optimization gap as one assertion type, not a separate subsystem)

Model matrix config, `.skillci.yaml` at repo root:

```yaml
models: [claude-sonnet-5, claude-opus-4-8, claude-haiku-4-5]
fail_on: regression   # vs "any_fail" vs "triggered_only"
```

`fail_on: regression` is the default specifically to avoid the "flaky CI nobody trusts" trap:
CI fails only on a *new* break vs. last known-good, not on every non-deterministic miss.

## 5. Self-growing eval loop

When `skillci regress` finds a failure with no corresponding eval case in history (a model
upgrade silently broke something uncovered), it does not just fail — it writes a proposed new
eval case capturing the failing prompt/behavior to `evals/_generated/*.yaml`, pending author
approval via `skillci accept <case-id>`.

This is the direct mechanical analogue of a bug → new invariant loop: a caught regression
becomes a permanent test, so the same gap can't silently regress twice. This is the project's
core differentiator — no existing eval tool in the space does this; they are static suites.

## 6. CLI surface

```
skillci init                 # scaffold .skillci.yaml + evals/ in a skill folder
skillci check [path]         # lint only, no API calls, fast/free, pre-commit-able
skillci eval [path]          # run eval suite against default model, no matrix
skillci regress [path]       # full matrix + diff vs history — the CI-gating command
skillci accept <case-id>     # promote a _generated eval case into the real suite
skillci badge [path]         # (re)generate SVG badge from latest history.json
```

Lint rules, MVP set only (expand later from real user reports, not speculative coverage):
- valid YAML frontmatter, required `name`/`description` present
- `description` length within Anthropic's documented trigger-matching budget
- referenced `references/`/`scripts/`/`assets/` files actually exist
- cheap regex pass for committed secrets/API keys (not a full security scanner — deep
  scanning is explicitly deferred, see §9)

Badge: generated SVG, shields.io-style. States: `passing` (green, all models) / `partial`
(yellow, N/M models) / `regressed` (red). Committed to the repo by the Action — no external
image-host dependency, no hotlink-availability failure mode.

## 7. Dashboard + ingestion API

Separate deployable from the CLI (keeps the Go binary dependency-free). Stack choice (e.g.
TS/Next.js vs Go+HTMX) deferred to implementation time — not load-bearing for this design.
Postgres for time-series pass/fail per skill/model/commit.

`POST /api/v1/results` — CLI sends `{repo, skill_name, commit_sha, model, pass,
eval_results[]}` on `skillci regress --upload` (opt-in only, never automatic — no silent
telemetry). Auth via a `skillci login` device-flow token, one per GitHub repo.

Public page: `skillci.dev/s/<owner>/<repo>/<skill>` — current badge state per model,
historical trend line, link to source. The shareable/screenshot-able artifact that drives the
virality loop (same pattern as codecov/Chromatic badges).

Leaderboard: `skillci.dev` root — opted-in skills sortable by pass rate / model coverage /
recency. Explicitly not a marketplace: no install flow, no ratings. A trust/compatibility
signal layer only, same distinction caniuse.com has from a browser directory.

Privacy default: `skillci regress` without `--upload` works fully standalone forever —
dashboard is additive, never required.

## 8. Error handling

- API failure (rate limit, auth, model unavailable) during `regress` → retry with backoff,
  then mark that model's row `error` (distinct from `fail` — doesn't trigger a false
  regression or gate CI on an Anthropic outage)
- Malformed `SKILL.md` → `check` fails fast with file:line, never reaches eval stage
- Dashboard upload failure → warn, exit 0 — a dashboard hiccup must never break someone's CI

## 9. MVP scope

**v1 (in):** `check`, `eval`, `regress` with full model matrix and the self-growing eval
loop, badge generation, GitHub Action, dashboard + ingestion API + public skill pages +
leaderboard.

**Explicitly deferred (v2+):** `skillci scan` (deep security scanning, ToxicSkills-style —
research shows 36% of scanned skills have security flaws, real problem, but distinct research
investment from CI consolidation), skill-to-skill dependency/versioning, non-Anthropic model
providers in the matrix (format is cross-agent but v1 targets Claude models only, matching the
sharpest confirmed gap).

**Permanently out of scope:** marketplace/install/ratings features — zero moat, saturated
space, ruled out on research grounds, not a sequencing decision.

## 10. Testing strategy (for the tool itself)

- Go table-driven unit tests for lint rules + YAML parsing — fast, no API calls
- Eval/regress engine tested against fixture skills with recorded API responses
  (cassette/golden-file style) — no live API calls in the project's own CI
- Project's CI dogfoods `skillci check` against its own `/examples` skills
