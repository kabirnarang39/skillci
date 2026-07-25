# LLM-as-Judge Hardening — Design

## Context

skillci's `Judge`/`JudgeStrict`/`judge_model` assertion (shipped in v0.1.0,
`internal/runner/runner.go`) works and is well-integrated — non-strict by
default, skips when other assertions already failed or a flake retry fired,
batches all criteria into one API call, 13 tests including real interaction
coverage with fuzz/snapshot/flake_retries.

Compared against industry judge implementations (deepeval's `GEval`,
promptfoo's `llm-rubric`, both built on the G-Eval technique), three gaps
were identified by direct code read:

1. No chain-of-thought reasoning before verdict — the single biggest gap.
   G-Eval's core finding is that forcing reasoning before a verdict
   materially improves judge/human correlation. Today's prompt asks for a
   bare `PASS`/`FAIL` with only a short reason on `FAIL`, nothing on `PASS`.
2. No caching — every judge call hits the API fresh every `regress` run.
   `fuzz_llm` already solved an adjacent problem (`.skillci/fuzz-llm-cache.json`,
   generate-once-cache-forever) but judge's input (a fresh case-model
   response) isn't a fixed, reusable prompt library the way fuzz_llm's
   mutations are.
3. No self-consistency sampling for the judge itself. `flake_retries`
   protects against the *case model's* sampling noise via majority vote
   across reruns; nothing protects against the *judge model's* own
   sampling variance on a borderline verdict — an asymmetry given
   skillci's own stated premise that a judge model can drift/be noisy too.

This spec hardens the existing feature. It does not introduce a new
top-level assertion type — `Judge`/`JudgeStrict`/`judge_model` keep their
current meaning and default behavior; every addition here is either
transparent (caching) or opt-in with a default that reproduces today's
exact behavior (`judge_mode: batched`, `judge_samples: 1`).

## 1. Chain-of-thought reasoning (unconditional)

Applies to every judge call, no new config. The judge prompt changes to
require a reasoning block per criterion before its verdict line:

```
SKILLCI_JUDGE_REASONING: <criterion name>: <step-by-step reasoning>
SKILLCI_JUDGE: <criterion name> = PASS
```

or

```
SKILLCI_JUDGE_REASONING: <criterion name>: <step-by-step reasoning>
SKILLCI_JUDGE: <criterion name> = FAIL: <short reason>
```

`parseJudgeVerdicts` is extended to capture the reasoning block and store
it in `JudgeFinding.Reason` (existing field, no rename — today it's
populated only for `FAIL`; after this change it's populated for `PASS`
too). The verdict line (`SKILLCI_JUDGE: ...`) remains the sole source of
truth for pass/fail; a malformed or missing reasoning block never blocks
verdict parsing — reasoning capture is best-effort, verdict capture is not.

A criterion with a verdict line but no matching reasoning block still
parses successfully (reasoning empty string) — this keeps the parser
tolerant of a judge model that doesn't perfectly follow the reasoning
format, without discarding a well-formed verdict over it.

## 2. Batching mode: `judge_mode`

New field, two levels:

- **Global**, `.skillci.yaml`: `judge_mode: batched | isolated`. Unset
  defaults to `batched` — today's behavior and cost profile, unchanged.
- **Per-case**, `assert.judge_mode` on `Assertions` (sibling to `judge`,
  `judge_strict`): overrides the global value for that case when set.

`batched` (default): all criteria go into one judge API call, as today —
now with the reasoning-block format from §1.

`isolated`: one judge API call per criterion. Each call gets full,
unshared reasoning context — no risk of one criterion's reasoning bleeding
into another's within the same generation. Cost/latency scales with
criterion count; offset by the caching layer in §3, and by `isolated`
being an explicit opt-in for cases where judge accuracy matters enough to
pay for it.

## 3. Caching (transparent, no new flag)

`.skillci/judge-cache.json`, parallel to the existing
`.skillci/fuzz-llm-cache.json`.

**Granularity**: one cache entry per judge *call*, not per criterion. In
`isolated` mode a call covers one criterion; in `batched` mode a call
covers every criterion in the case's `judge` list together — the cache
follows whichever grouping `judge_mode` already puts into one API call,
since a batched call returns all its criteria's verdicts atomically and
can't be topped up for just one of them.

**Key**: `hash(judgeModel + the exact set of criteria in this call + response content)`.
Unlike fuzz_llm's fixed prompt library, judge's input (the case model's
response) is different on most runs, so this cache's hit rate is lower —
it hits on: re-running the same commit in CI (retry, or a second
`regress` invocation without code changes), `bisect` re-testing a commit
it has already tested at this (case, model) pair, and deterministic/
greedy-decoded case models that reproduce byte-identical responses across
runs. Changing the criteria set (editing `judge:` in the eval YAML) is a
deliberate cache miss — the criteria are part of the key.

**Value**: the list of *raw per-call* samples for that key (see §4), each
holding every criterion's verdict from that call — not just a final vote.
This lets `judge_samples` increase between runs without invalidating
already-cached samples (§4 explains the reuse rule).

**Eviction**: LRU (least-recently-*used*, not least-recently-inserted —
matters for `bisect`, which revisits the same commits across steps of its
search), capped at 500 entries. Chosen over unbounded (fuzz-llm-cache's
precedent) because judge-cache's entries have a much lower expected reuse
rate — most will be written once and never hit again — so unbounded
growth would accumulate dead weight in a git-committed file faster than
fuzz-llm-cache does.

No bypass flag — consistent with fuzz-llm-cache having none today. Delete
the cache file manually if a forced re-judge is ever needed.

## 4. Self-consistency: `judge_samples`

New field, `assert.judge_samples` on `Assertions` (per-case only — no
global equivalent, matching `flake_retries`' shape). `*int`, default nil
= 1 sample = today's exact behavior (single call, verdict trusted
directly, no voting logic invoked).

When set to N > 1: take N samples of the judge *call* (§3's granularity —
one call per criterion in `isolated` mode, one call for all criteria in
`batched` mode), each drawn from cache first, then live API calls for any
shortfall. Majority-vote PASS/FAIL independently per criterion, using
that criterion's verdict from each of the N samples. A tied vote (only
reachable with even N) is informational only unless `judge_strict` is
also true, in which case it counts as a failure — mirrors `flake_strict`'s
handling of an unresolved flake tie exactly.

**Partial cache reuse**: the cache value for a given key is a list of
call-samples, independent of what `judge_samples` was set to when they
were written. On lookup: if the cache already holds ≥ N samples for the
key, reuse the first N and make zero API calls. If it holds M < N, make
(N − M) additional calls, append the new samples to the cached list (up
to the 500-entry cap's per-key storage), then vote over all N. Raising
`judge_samples` in a later run never invalidates or wastes prior calls.

**`JudgeFinding` additions**: `SampleCount int` and `PassCount int`, both
zero/omitted in output when N = 1 (the common case) — CLI and dashboard
only render the vote tally when there's a vote to show.

## Data flow (isolated example, judge_samples: 3)

```
RunCase → (other assertions pass, no flake retry) → for each criterion:
  cache lookup (judgeModel, criterion, response) → 3 samples cached?
    yes → use them, 0 API calls
    no (e.g. 1 cached) → 2 live judge calls → append to cache → 3 samples
  majority-vote the 3 samples → JudgeFinding{Passed, Reason, SampleCount:3, PassCount:2 or 3}
→ JudgeStrict set and any criterion failed? → hard failure
```

## Error handling

- Missing `judge_model` with `judge` criteria configured: unchanged, fails
  loud at case-run time with the existing clear error.
- A verdict line missing or malformed for a criterion: unchanged, treated
  as `FAIL` with an explanatory reason — never silently dropped.
- A reasoning block missing or malformed: never fails the case — reasoning
  is best-effort context, not a correctness gate.
- Cache file missing, empty, or corrupt: treated as an empty cache (same
  precedent as fuzz-llm-cache.json) — never a hard error, always falls
  back to live calls.

## Testing

- Extend all 13 existing judge tests for the new reasoning-block format
  (verdict parsing must still pass with reasoning present; must still
  pass — as empty `Reason` — if the judge omits the reasoning block).
- New: `judge_mode` batched-vs-isolated call-count tests (assert exact
  number of judge API calls made for a multi-criterion case in each
  mode), and global-vs-per-case-override precedence.
- New: cache hit/miss, eviction-at-500-cap (LRU-order, not
  insertion-order), and corrupt-cache-file fallback.
- New: `judge_samples` majority vote (odd N), tied vote at even N under
  both `judge_strict` true/false, and partial-cache-reuse when
  `judge_samples` increases between two runs against the same cache file.
- Re-run the existing judge × flake_retries × snapshot × fuzz interaction
  tests (`TestRunCaseJudgeAndFuzzBothEnabledProduceBothArtifacts`,
  `TestRunCaseJudgeSkippedWhenFlakeRetriesFired`,
  `TestRunMatrixJudgeSkippedWhenFlakeRetriesFiredMakesNoJudgeCall`, etc.)
  unmodified in behavior — this feature must not change when judging is
  skipped, only how it behaves once it runs.
