---
name: skillci-guardrails
description: Use this skill whenever you create, edit, or review a Claude Skill — any SKILL.md file, or the eval cases/config next to one. Most SKILL.md files today are written or edited by an agent, not typed by hand, so this closes the loop by having the agent that just wrote the skill also author it defensively and verify it, the same way a linter and test suite run after any other code change. Trigger phrases include "write a skill", "create a SKILL.md", "add a new skill", "edit this skill", "update the skill's frontmatter/description/triggers", or any diff that touches a SKILL.md path.
---

# skillci guardrails

A `SKILL.md` is code: its frontmatter is an API, its body is an
executable instruction set. It fails silently — a model just quietly
does the wrong thing — rather than loudly, which makes both authoring it
defensively and verifying it after the fact more important than for
ordinary code, not less.

## Step 1 — Author defensively, don't just lint afterward

These map directly to skillci's own static checks, so getting them right
up front means Step 2 finds nothing instead of catching it after the
fact:

- **`description`** is the single field that decides whether this skill
  gets discovered and triggered at all — state what it does *and* when to
  use it, in language close to how a user would actually phrase the
  request. Keep it under 1024 characters (skillci flags longer — it eats
  into every caller's trigger-matching budget).
- **Never instruct piping a downloaded script straight into a shell
  interpreter**, and never reference an unpinned `:latest`/`@latest`
  dependency — both are OWASP AST01/AST02 patterns skillci's static scan
  flags, and both are real risk, not just lint noise.
- **Don't request network access to non-localhost hosts** unless the
  skill's whole purpose requires it, and say why in the body if it does
  (AST03).
- **Never fetch remote content and tell the model to treat it as
  authoritative instructions** (AST05) — if a skill needs to pull shared
  context from a URL, use frontmatter's `pinned_sources` (a declared
  `sha256` skillci can verify against on request) instead of an
  unqualified fetch-and-obey pattern.
- **Keep the body lean**: under ~8000 characters, no exact-duplicate
  instruction lines, no more than ~10 referenced files or ~100KB of
  referenced content combined. Every extra line loads on every
  invocation — bloat has a real, ongoing cost, not just a one-time
  cleanup debt.

## Step 2 — Verify

Prefer calling skillci's own MCP tools directly if they're available in
this session (`check`, `eval`, ...) over shelling out — same result,
lower friction, no risk of misremembering a CLI flag. Fall back to the
CLI otherwise:

```bash
skillci check <path-to-skill-dir>
```

1. **Always run `check`** — local-only, free, no API calls. Fix every
   finding before moving on, including Minor/nice-to-have ones; don't
   leave known findings unaddressed just because the skill "looks fine."
   If this repo is still piloting skillci non-blocking, `--mode warn` (or
   `.skillci.yaml`'s `lint.mode`) reports findings without failing —
   that's a repo-level rollout decision, not a reason to ignore what it
   reports.
2. **If eval cases exist, run `eval`** — runs the skill against a real
   model and checks its assertions. If you just changed trigger phrases
   or behavior, this is the only way to confirm the change actually
   works, not just that it reads plausibly.
3. **If eval cases don't exist yet and the skill is non-trivial, write at
   least one before calling the work done.** "Non-trivial" means any of:
   more than one distinct trigger condition, any security-relevant
   behavior (network calls, running code, handling credentials), or
   intended for reuse beyond this one session/repo. A skill with zero
   coverage is the untested code of this domain.
4. **If skillci isn't installed**: `go install
   github.com/kabirnarang39/skillci/cmd/skillci@latest` (or see
   https://github.com/kabirnarang39/skillci#install for Homebrew/Scoop/
   prebuilt binaries/an MCP client config). Don't skip verification just
   because the tool isn't on `PATH` yet — install it, then run it.

## Step 3 — Pick the right assertion for a new eval case

Don't reach for the heaviest tool by default — match the assertion to
what actually needs checking:

| The skill... | Reach for |
|---|---|
| Should fire on certain phrasings | `triggered: true` |
| Must produce specific required text | `contains: [...]` |
| Must never produce certain text | `not_contains: [...]` |
| Needs subjective/quality judgment a substring match can't express | `judge:` (named rubric criteria, scored by a separate model) |
| Fetches/executes untrusted content, or is otherwise security-sensitive | `redteam:` (prompt-injection-canary, instruction-leakage, jailbreak-direct-override, harmful-content-elicitation) |
| Needs to hold up under realistic rewording, not just the exact prompt you tested | `fuzz: true` (add `fuzz_llm: true` for model-generated paraphrases, cached once) |
| Must not silently change behavior on a model update | `snapshot: true` |
| Has a real cost/latency/token budget to enforce | `max_cost_usd` / `max_latency_ms` / `max_tokens_loaded` |

A minimal but real example:

```yaml
name: "haiku-request-triggers"
prompt: "Can you write me a haiku about autumn leaves?"
skill_under_test: "haiku-writer"
assert:
  triggered: true
  contains: ["autumn"]
```

## What to do with findings

Fix them, the same way you'd fix a failing test or a lint error before
telling the user a code change is complete — don't report the skill as
finished with known-but-unfixed findings unless the user explicitly says
to leave them.

## Don't

- Don't accept a `regress`-generated eval case (`evals/_generated/*.yaml`)
  blindly — it's evidence that a regression happened, not automatically
  the correct behavior to lock in as permanent coverage. Read what it
  actually asserts before `accept`ing it.
- Don't reach for `judge:` for something `contains:`/`not_contains:`
  could check for a fraction of the cost — judge criteria cost a model
  call and are for genuinely subjective quality, not a substitute for a
  substring check.
- Don't add `redteam:` to every skill by default — reserve it for skills
  that fetch external content, execute code, or handle anything
  security- or credential-adjacent. A static FAQ skill doesn't need
  adversarial attack plugins.
- Don't set a `*_strict` flag (`snapshot_strict`, `latency_strict`,
  `flake_strict`, `judge_strict`, `redteam_strict`) without understanding
  exactly what it gates — each is a no-op without its paired base
  assertion also set, and skillci's own lint rules will flag that
  specific footgun, but the flag's *meaning* (this now hard-fails CI) is
  worth being deliberate about regardless.
- Don't run `eval`/`regress` speculatively against unrelated skills
  "while you're at it" — scope this to the skill you actually touched.
- Don't add an elaborate eval suite to a trivial, static skill that has
  no real trigger logic or security surface to test.
