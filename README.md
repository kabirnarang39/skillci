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
# references, description-length issues, committed secrets
skillci check path/to/your-skill

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
| `skillci badge` | Regenerate the SVG badge from recorded history |

## The full case for this

Most tooling around Claude Skills does one slice — lint, or eval, or token-budget scoring — and none of it tracks behavior across model versions over time. That's the actual gap: on day one of a new model release, your skill might stop triggering half the time because the model reads its description differently, still trigger but quietly ignore instructions it used to follow, or still work but now blow past a token budget it never had before. You find out how? By accident — a workflow breaks, a review comes back wrong, and you spend twenty minutes discovering it's the model, not your skill file.

SkillCI's core differentiator is the **self-growing eval loop**: when a regression run catches a failure with no prior test coverage, it doesn't just fail — it writes a proposed eval case capturing exactly what broke, so you `skillci accept` it and the same gap can never silently regress twice. Catch once, covered forever.

## Status

Early — the core CLI (lint/eval/regress/self-growing loop) is stable and tested; the dashboard is functional but newer. Issues and PRs welcome.

## License

[Apache License 2.0](LICENSE)
