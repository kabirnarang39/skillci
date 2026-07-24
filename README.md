# SkillCI

**Regression testing for Claude Skills.** When a model update silently changes how your skill behaves, SkillCI catches it in CI — and turns the failure into a permanent test case, automatically.

[![CI](https://github.com/kabirnarang39/skillci/actions/workflows/ci.yml/badge.svg)](https://github.com/kabirnarang39/skillci/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](go.mod)

![SkillCI demo: skillci check, regress across a model matrix, catching an uncovered failure, generating an eval case, and accepting it](.github/assets/demo.gif)

A skill fails against a model it's never been tested on → SkillCI doesn't just report red, it **writes the missing test case for you** (`evals/_generated/...`) so `skillci accept` turns it into permanent coverage. That loop — catch once, covered forever — is the whole point.

## Why

You write a Claude Skill. It works today. Six months from now, Anthropic ships a new model, and nobody tested your skill against it first — because until now, no tool did that automatically. It might stop triggering, ignore instructions it used to follow, or blow past a token budget you never knew it had. You find out by accident, not by CI.

Every other kind of software has a safety net for this — tests that fail the moment behavior changes. Skills have had none. SkillCI is that safety net: write down what a skill should do once, and get an automated answer to "did it survive the last model release" instead of finding out the hard way. [Full rationale below.](#the-full-case-for-this)

## Install

```bash
go install github.com/kabirnarang39/skillci/cmd/skillci@latest
```

Requires Go 1.25+. An `ANTHROPIC_API_KEY` is needed for `eval`/`regress` (not for `check`, which is local-only and free).

## Quick start

```bash
# Scaffold config + an example eval case inside your skill's folder
skillci init path/to/your-skill

# Lint SKILL.md — no API calls, catches malformed frontmatter, missing
# references, description-length issues, committed secrets, a
# first-layer static security scan (OWASP Agentic Skills Top 10:
# malicious payloads, over-privileged access, insecure metadata parsing,
# cross-platform format issues), and basic "skill bloat" warnings
# (oversized body, duplicate instructions, too many/too-large referenced
# files)
skillci check path/to/your-skill
```

`skillci check`'s security rules are mapped directly to
[OWASP's Agentic Skills Top 10](https://owasp.org/www-project-agentic-skills-top-10/):
malicious payloads, over-privileged access requests, insecure frontmatter
parsing, and cross-platform path issues. This is a first-layer static scan,
not a malware scanner — obfuscated or natural-language-only attacks can
bypass pattern matching, a limitation OWASP itself documents (AST08).

`skillci check` also flags basic skill bloat: an oversized `SKILL.md` body
(over 8000 characters — every extra instruction is loaded on every
invocation), exact-duplicate instruction lines (copy-paste bloat), and
skills that reference too many files or too much referenced-file content
(over 10 files or 100KB combined). These are fixed thresholds, not
user-configurable, and — like the security rules — purely local pattern
matching, not a judgment call about whether a skill is *good*, just
whether it's carrying more than it needs to.

```bash
# Run the eval suite against one model
skillci eval path/to/your-skill --model claude-sonnet-5

# Run the full regression matrix (every model in .skillci.yaml), diffed
# against the last known-good run — fails CI only on a *new* regression
skillci regress path/to/your-skill
```

An eval case (`evals/*.yaml`) looks like this:

```yaml
name: "haiku-request-triggers"
prompt: "Can you write me a haiku about autumn leaves?"
skill_under_test: "haiku-writer"
assert:
  triggered: true
  contains: ["autumn"]
  max_tokens_loaded: 3000
```

When `regress` finds a failure with zero prior coverage for that case+model, it writes a proposed case to `evals/_generated/` instead of just failing:

```
[FAIL] unrelated-request-should-not-trigger (claude-sonnet-5)
    triggered = true, want false
proposed new eval case: evals/_generated/unrelated-request-should-not-trigger-generated-claude-sonnet-5.yaml
```

```bash
skillci accept unrelated-request-should-not-trigger-generated-claude-sonnet-5
```

promotes it into `evals/`, so it's a tracked regression guard from then on.

For cases where you want to know if a skill's actual response *content*
drifts between runs — not just whether it triggered or hit the right
substrings — add `snapshot: true`:

```yaml
name: "haiku-tone-check"
prompt: "Write a haiku about the ocean."
skill_under_test: "haiku-writer"
assert:
  triggered: true
  snapshot: true
```

The first run captures the response as a per-model golden baseline
(`evals/<case>.<model>.golden.txt`). Every later run word-diffs the new
response against it and shows what changed — without failing CI, unless
you also set `snapshot_strict: true`. This is deliberately informational
by default: it tells you *that* something changed, the same way any
snapshot-testing tool (Jest, ApprovalTests) does — not whether the change
is good or bad. You decide, then run:

```bash
skillci diff my-case --path path/to/your-skill --model claude-sonnet-5   # inspect
skillci accept my-case --model claude-sonnet-5                    # promote
```

To check whether a skill's trigger behavior is robust to rewording — not just
whether the exact eval prompt fires it — add `fuzz: true` to a case that also
asserts `triggered`:

```yaml
name: "haiku-request-triggers"
prompt: "Can you write me a haiku about autumn leaves?"
skill_under_test: "haiku-writer"
assert:
  triggered: true
  fuzz: true
```

Every run generates deterministic paraphrases of the prompt — synonym swaps,
negation insertion, sentence reordering, and unrelated leading context — and
checks whether the skill still triggers (or doesn't) the way `triggered`
expects. No LLM writes the paraphrases; the mutations are fixed, non-random
string transformations, so a fuzz run costs nothing beyond the extra model
calls it makes. Note that `regress` fuzzes every model in your configured
matrix, so total API calls scale as `models × fuzz-enabled cases × (1 + up
to 11 mutations)` — the worst case is a 3-sentence prompt that hits all
four operators (1 synonym-swap + 2 negation + 5 non-identity 3-sentence
reorderings + 3 context-prefix = 11 mutations), for 12 calls per model per
case including the primary run. Like `snapshot`, this is informational by default:

```
[FUZZ] 2/9 mutations flipped trigger behavior
  negation: "Can you don't write me a haiku about autumn leaves?" -> triggered=false (want true)
```

Add `fuzz_strict: true` to fail CI on a flip. Run it standalone with:

```bash
skillci fuzz path/to/your-skill --model claude-sonnet-5
```

or let it run automatically as part of `skillci regress` for any case that
sets `fuzz: true` — no separate invocation needed for full coverage.

Model responses aren't fully deterministic — even at low temperature,
sampling variance can make a `triggered`/`contains`/`not_contains` check
fail on an otherwise-healthy skill. For cases where that matters more
than the extra API cost, `flake_retries` reruns a failing case's trigger
checks and takes a majority verdict instead of trusting a single sample:

```yaml
name: "haiku-request-triggers"
prompt: "Can you write me a haiku about autumn leaves?"
skill_under_test: "haiku-writer"
assert:
  triggered: true
  flake_retries: 2
```

Only fires when the FIRST attempt's trigger checks fail — a passing case
never pays the extra cost. Up to `1 + flake_retries` total attempts are
made, stopping early once a majority is mathematically decided (e.g. 2
failing attempts out of 3 possible stops before the 3rd call). Budget
assertions (`max_tokens_loaded`, `max_output_tokens`, `max_latency_ms`,
`max_cost_usd`) are never retried — they're checked once, same as
always, since rerunning can't change a token-count-derived cost or
latency reading into something more "correct."

An odd `flake_retries` value can tie (e.g. `flake_retries: 1` → 2 total
attempts, 1-1). A tie is informational only by default:

```
[RETRY] triggered check unstable after 2 attempts — 1/2 passed (tie), informational only unless flake_strict is set
```

Add `flake_strict: true` to fail CI on an unresolved tie instead.

Deterministic assertions can't check response *quality* — tone, empathy,
whether an explanation is actually clear. For that, `judge` sends the
response to a separate model with a rubric of named criteria and takes
its verdict, informational by default:

```yaml
name: "haiku-request-triggers"
prompt: "Can you write me a haiku about autumn leaves?"
skill_under_test: "haiku-writer"
assert:
  triggered: true
judge:
  - name: tone
    criterion: "Is the response warm and encouraging, not clinical?"
  - name: imagery
    criterion: "Does the haiku use at least one concrete visual image?"
```

Requires `judge_model` in `.skillci.yaml` — deliberately a separate model
from the ones under test, never the model judging itself, since a model
can't reliably judge its own drift:

```yaml
judge_model: claude-opus-4-8
```

All criteria must pass for the judge step to pass. Every criterion is
evaluated together in a single extra API call, regardless of how many
you list. Judging only runs once every other assertion has already
passed — it's the last check, not a substitute for `triggered`/
`contains`. Add `judge_strict: true` to fail CI on a failing criterion;
without it, a failure just prints:

```
[JUDGE] 1/2 criteria failed
  tone: FAIL — reads as clinical rather than warm
```

This is deliberately the most opt-in, most secondary assertion type in
skillci: the whole premise of this tool is catching model drift
deterministically, and an LLM judge is the one technique that can't
judge itself reliably when the judge model is also something that might
drift. Reach for the deterministic assertions first; add `judge` only
for what genuinely can't be checked any other way.

When a case that used to pass starts failing, `skillci bisect` finds which
commit in your skill's own git history broke it — the same binary-search
idea as `git bisect`, aimed at your skill instead of your code, holding the
current model fixed throughout:

```bash
skillci bisect my-case --path path/to/your-skill --model claude-sonnet-5
```

With no `--good`/`--bad` flags, it looks up the last recorded passing run
for that case in `.skillci/history.json` and the most recent recorded run,
and searches the commits between them that touched the skill's files —
checking out each candidate into a disposable `git worktree` (your actual
working tree is never touched) and re-running the case against it:

```
verifying good/bad endpoints...
  9f8e7d6 — fail
  a1b2c3d — pass
good: a1b2c3d (2026-06-01) — passes
bad:  9f8e7d6 (2026-07-20) — fails
7 candidate commits, up to 3 more API calls
bisecting...
  4f3a2b1 — pass
  8c7d6e5 — fail

culprit: 6a5b4c3d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3
author:  Kabir Narang <kabir.narang@zinier.com>
date:    2026-07-10
message: tighten haiku-writer's tone guidance

--- SKILL.md (6a5b4c3d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3^)
+++ SKILL.md (6a5b4c3d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3)
@@ ...
- Write a haiku about the requested topic.
+ Write a haiku about the requested topic, staying strictly formal in tone.
```

`skillci regress` also prints a `skillci bisect ...` suggestion inline
whenever it detects a new regression, so you don't need to remember the
command yourself.

For cost and latency budgets, three more assertions are available:

```yaml
name: "cost-budget-case"
prompt: "Write a haiku about autumn."
skill_under_test: "haiku-writer"
assert:
  triggered: true
  max_output_tokens: 500
  max_latency_ms: 3000
  max_cost_usd: 0.01
```

`max_output_tokens` and `max_cost_usd` are hard caps — like `max_tokens_loaded`,
exceeding either fails the case immediately. `max_cost_usd` needs a pricing
entry in `.skillci.yaml` — skillci never hardcodes or guesses prices, since
Anthropic can reprice without notice:

```yaml
pricing:
  claude-sonnet-5:
    input_per_million: 3.0
    output_per_million: 15.0
```

A case asserting `max_cost_usd` for a model with no pricing entry fails
loudly, naming the missing model, rather than silently skipping the check.

`max_latency_ms` is the one exception to the hard-cap rule: latency reflects
network and inference variance, not what the skill actually did, so an
exceeded cap is informational only — printed, not failed — unless you also
set `latency_strict: true`.

Eval cases can also carry free-form `dimensions` for slicing results — e.g.
a case representing your enterprise-tier traffic or a specific language
variant:

```yaml
name: enterprise-billing-question
prompt: "..."
assert:
  triggered: true
dimensions:
  segment: enterprise
  language: es
```

By default this only affects reporting — `skillci regress` groups its
output by dimension (`--- by dimension ---`) so a cratering segment is
visible at a glance instead of buried in a flat case list. To make a
specific slice's failures always fail CI regardless of the global
`fail_on` policy, name it in `.skillci.yaml`:

```yaml
fail_on: triggered_only
strict_dimensions:
  segment: [enterprise]
```

Any case tagged `segment: enterprise` now fails CI on any failure, even
though the rest of the suite is gated more loosely.

## GitHub Actions

```yaml
- uses: kabirnarang39/skillci/.github/actions/skillci@main
  with:
    path: path/to/your-skill
    anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

Gates CI on **new** regressions only — a flaky non-deterministic miss won't fail your build every time. Generates and commits a status badge (`passing` / `partial` / `regressed`).

## Optional: hosted dashboard

`cmd/skillci-server` is a small Postgres-backed HTTP server that turns `skillci regress --upload` results into a public, per-skill compatibility history and leaderboard — the "does my skill still pass on the model shipped this week" trust signal, shareable the way a codecov badge is. Entirely opt-in; the CLI works standalone forever without it.

```bash
export SKILLCI_DATABASE_URL="postgres://..."
export SKILLCI_INGEST_TOKEN="a-shared-secret"
go run ./cmd/skillci-server
```

## Commands

| Command | What it does |
|---|---|
| `skillci init` | Scaffold `.skillci.yaml` and an example eval case |
| `skillci check` | Lint `SKILL.md` — local only, no API calls |
| `skillci eval` | Run the eval suite against one model |
| `skillci regress` | Run the full model matrix, diff vs. last known-good, gate CI |
| `skillci accept` | Promote a generated eval case into the permanent suite |
| `skillci diff` | Show a case's pending snapshot change against its golden baseline |
| `skillci fuzz` | Run mutation-based robustness testing for fuzz-enabled eval cases |
| `skillci bisect` | Binary-search a skill's git history for the commit that broke an eval case |
| `skillci badge` | Regenerate the SVG badge from recorded history |

## The full case for this

Most tooling around Claude Skills does one slice — lint, or eval, or token-budget scoring — and none of it tracks behavior across model versions over time. That's the actual gap: on day one of a new model release, your skill might stop triggering half the time because the model reads its description differently, still trigger but quietly ignore instructions it used to follow, or still work but now blow past a token budget it never had before. You find out how? By accident — a workflow breaks, a review comes back wrong, and you spend twenty minutes discovering it's the model, not your skill file.

SkillCI's core differentiator is the **self-growing eval loop**: when a regression run catches a failure with no prior test coverage, it doesn't just fail — it writes a proposed eval case capturing exactly what broke, so you `skillci accept` it and the same gap can never silently regress twice. Catch once, covered forever.

`skillci bisect` is real `git worktree`-based binary search over a skill's own commit history — not marketing language for "narrowing a failing axis." Other tools have converged on similar ideas from different angles (prompt-version stores, internal bisection over non-git state); skillci's is the version that runs against your actual repo, with Claude-Skills-native assertions.

## Status

Early — the core CLI (lint/eval/regress/self-growing loop) is stable and tested; the dashboard is functional but newer. Issues and PRs welcome.

## License

[Apache License 2.0](LICENSE)
