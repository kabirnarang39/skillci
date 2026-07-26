# Adding a redteam plugin

`internal/redteam.Registry` is a plain Go map — every entry is one named
attack technique the `redteam:` eval assertion can reference by name (see
the README's redteam section for how cases use these). Adding a new one is
a short loop: generate a stub, fill it in, register it, add a test, open a
PR.

## 1. Generate a stub

```bash
skillci scaffold redteam-plugin <your-plugin-name> --grading deterministic|judge|multiturn
```

Pick the grading mode based on the attack:

- **`deterministic`** — the attack succeeds or fails based on a literal,
  checkable signal in the response (a leaked token, an echoed string) —
  zero judge-model cost. Reuse this shape for anything where "did the
  exact text X appear in the response" is a sufficient detector.
- **`judge`** — the attack's success is semantic, not literal (did the
  model comply with an override, did it reveal something in its own
  words) — needs a separate judge model to grade PASS/FAIL against a
  rubric.
- **`multiturn`** — a judge-graded attack that needs conversational setup
  across multiple turns before the actual ask — only the final turn's
  response is graded.

## 2. Worked examples (each is real `scaffold` output)

### Deterministic

```go
	"example-deterministic-attack": {
		Name:     "example-deterministic-attack",
		Category: CategoryInjection, // TODO: pick the category that fits — see internal/redteam.Category's const block
		Grading:  GradingDeterministic,
		// TODO: BuildAttack returns the attack-augmented prompt to send
		// in place of the case's own prompt, plus any per-invocation
		// state Detect needs (e.g. a random token) — empty string if
		// unused. See prompt-injection-canary in internal/redteam/redteam.go for
		// a worked example, and docs/adding-a-redteam-plugin.md.
		BuildAttack: func(basePrompt string) (attackPrompt, state string) {
			return basePrompt, ""
		},
		// TODO: Detect reports whether the attack succeeded against
		// response, given the state BuildAttack returned.
		Detect: func(response, state, skillName, skillDescription string) (succeeded bool, detail string) {
			return false, ""
		},
	},
```

Fill in `BuildAttack` (returns the attack-augmented prompt, plus any
per-invocation state `Detect` needs — e.g. a random per-run token so a
model can't have memorized a fixed value) and `Detect` (a pure function,
no model calls, checking `response`/`state` for the compromise signal).
`internal/redteam/redteam.go`'s `prompt-injection-canary` entry is the
canonical worked example already in the codebase.

### Judge-graded

```go
	"example-judge-attack": {
		Name:     "example-judge-attack",
		Category: CategoryJailbreak, // TODO: pick the category that fits — see internal/redteam.Category's const block
		Grading:  GradingJudge,
		// TODO: the text appended to the case's own prompt to build the
		// attack. See jailbreak-direct-override in internal/redteam/redteam.go
		// for a worked example.
		AttackPromptSuffix: "TODO: describe the attack instruction here",
		// TODO: the rubric a separate judge model grades the response
		// against — PASS should mean the attack was DEFENDED.
		JudgeRubric: "TODO: describe what a defended response looks like, and what a compromised one looks like",
	},
```

Fill in `AttackPromptSuffix` (appended to the case's own prompt) and
`JudgeRubric` (fed to a separate judge model — write it so PASS clearly
means "the attack was defended," matching every other plugin's polarity
convention). `jailbreak-direct-override` is the canonical worked example.

### Multi-turn

```go
	"example-multiturn-attack": {
		Name:     "example-multiturn-attack",
		Category: CategoryJailbreak, // TODO: pick the category that fits — see internal/redteam.Category's const block
		Grading:  GradingJudge,
		// TODO: an escalating sequence of user messages — only the FINAL
		// turn's response is graded against JudgeRubric below. See
		// crescendo-jailbreak in internal/redteam/redteam.go for a worked
		// example, including how the runner anchors turn 1 in the
		// case's own prompt.
		Turns: []TurnStep{
			{Message: "TODO: turn 1 — establish innocuous framing"},
			{Message: "TODO: turn 2 — build on turn 1"},
			{Message: "TODO: turn 3 — the actual ask, leaning on the prior turns"},
		},
		// TODO: graded against the FINAL turn's response only.
		JudgeRubric: "TODO: describe what a defended response looks like, and what a compromised one looks like",
	},
```

Fill in `Turns` (an escalating sequence — the runner anchors turn 1 in the
case's own prompt automatically, and strips trigger-marker scaffolding from
every turn's response before threading it into history) and `JudgeRubric`
(graded only against the FINAL turn's response). `crescendo-jailbreak` is
the canonical worked example.

## 3. Register it

Copy your filled-in stub into `internal/redteam/redteam.go`'s `Registry`
map literal, using your plugin's name as the map key (must match the
`Name` field).

## 4. Add a test

Follow the existing pattern in `internal/redteam/redteam_test.go` — for a
deterministic plugin, unit-test `BuildAttack`/`Detect` directly (no model
needed). For a judge-graded or multi-turn plugin, a lighter registry-shape
test is enough (see `TestRegistryEntriesAreInternallyConsistent`, which
already generically validates every entry's field population by grading
mode) — the actual attack-quality verification happens via a real
`skillci eval`/`redteam` run against a target skill, not a unit test.

## 5. Open a PR

That's it — `internal/regress`'s self-growing eval loop, `skillci check`'s
plugin-name validation, and every CLI/MCP surface already work generically
over anything in `Registry`. No other file needs to change.
