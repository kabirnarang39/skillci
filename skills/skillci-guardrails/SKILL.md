---
name: skillci-guardrails
description: Use this skill whenever you create, edit, or review a Claude Skill — any SKILL.md file, or the eval cases/config next to one. Most SKILL.md files today are written or edited by an agent, not typed by hand, so this closes the loop by having the agent that just wrote the skill also verify it, the same way a linter runs after any other code change. Trigger phrases include "write a skill", "create a SKILL.md", "add a new skill", "edit this skill", "update the skill's frontmatter/description/triggers", or any diff that touches a SKILL.md path.
---

# skillci guardrails

You just created or edited a Claude Skill. Before treating that work as
done, verify it the way you'd run a linter and test suite after any other
code change — a `SKILL.md` is code: its frontmatter is an API, its body is
an executable instruction set, and it fails silently (a model just quietly
does the wrong thing) rather than loudly, which makes verification more
important, not less.

## What to run

1. **Always**: `skillci check <path-to-skill-dir>` — local-only, free, no
   API calls. Lints frontmatter, structure, and OWASP Agentic Skills
   Top 10-mapped security patterns. Fix every finding before moving on;
   don't leave Minor/nice-to-have findings unaddressed just because the
   skill "looks fine."
2. **If the skill has (or should have) eval cases**: `skillci eval
   <path-to-skill-dir>` — runs the skill against real models and checks
   `triggers:`/`contains:`/`judge:`/etc. assertions. If you just wrote new
   trigger phrases or changed the skill's behavior, this is the only way
   to confirm the change actually works, not just that it looks
   plausible.
3. **If eval cases don't exist yet and the skill is non-trivial** (has
   real trigger conditions, more than a couple of instructions, or any
   security-relevant behavior): write at least one eval case before
   calling the skill done. A skill with zero coverage is the untested
   code of this domain.
4. **If skillci isn't installed**: `go install
   github.com/kabirnarang39/skillci/cmd/skillci@latest` (or see
   https://github.com/kabirnarang39/skillci#install for Homebrew/Scoop/
   prebuilt binaries). Don't skip verification just because the tool
   isn't on `PATH` yet — install it, then run it.

## What to do with findings

Fix them, the same way you'd fix a failing test or a lint error before
telling the user a code change is complete — don't report the skill as
finished with known-but-unfixed `skillci check` findings unless the user
explicitly says to leave them.

## Don't

- Don't run `skillci eval`/`regress` speculatively against unrelated
  skills "while you're at it" — scope this to the skill you actually
  touched.
- Don't treat this as a reason to add elaborate eval suites to a trivial,
  static skill that has no real trigger logic to test.
