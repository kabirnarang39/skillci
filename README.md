# SkillCI

**CI for Claude Skills.** Lint, eval, and regression-test `SKILL.md` files against a matrix of Claude models — and catch it automatically when a model upgrade silently breaks a skill that worked yesterday.

[![CI](https://github.com/kabirnarang39/skillci/actions/workflows/ci.yml/badge.svg)](https://github.com/kabirnarang39/skillci/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](go.mod)

## Why

You write a Claude Skill. It works today — you ask something, it fires, it does the right thing.

Six months from now, Anthropic ships a new model. Nobody tests old skills against new models before they ship — that's not Anthropic's job, and until now there hasn't been a tool to make it yours either. So on day one of the new model, your skill might:

- stop triggering half the time, because the model reads its description differently
- still trigger, but quietly ignore instructions it used to follow
- still work, but now blow past a token budget you never knew it had

You find out how? By accident — a workflow breaks, a review comes back wrong, and you spend twenty minutes discovering it's the model, not your skill file.

Every other kind of software you ship has a safety net for this: tests that fail in CI the moment behavior changes. Skills have had none. **SkillCI is that safety net** — write down once what a skill should do, and get an automated, historical, CI-gated answer to "did it survive the last model release," instead of finding out the hard way.

## What makes it different

Most tooling around Claude Skills does one slice — lint, or eval, or token-budget scoring — and none of it tracks behavior across model versions over time. SkillCI's core differentiator is a **self-growing eval loop**: when a regression run catches a failure with no prior test coverage, it doesn't just fail — it writes a proposed eval case capturing exactly what broke, so you can `skillci accept` it and the same gap can never silently regress twice.

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

## Status

Early — the core CLI (lint/eval/regress/self-growing loop) is stable and tested; the dashboard is functional but newer. Issues and PRs welcome.

## License

[Apache License 2.0](LICENSE)
