package evalspec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// yaml tags below all carry omitempty: an unset assertion must not be
// written out as a literal `null`/`[]` when a Case is marshaled back to
// YAML (WriteGeneratedCases, for the self-growing eval loop) — the whole
// point of a generated case file is a human reviewing it before `accept`,
// and ~15 fields nobody set spelled out one per line is exactly the noise
// that review doesn't need. Doesn't affect reading a hand-written
// evals/*.yaml file at all (omitempty only changes marshaling behavior).
type Assertions struct {
	Triggered       *bool    `yaml:"triggered,omitempty"`
	Contains        []string `yaml:"contains,omitempty"`
	NotContains     []string `yaml:"not_contains,omitempty"`
	MaxTokensLoaded *int     `yaml:"max_tokens_loaded,omitempty"`
	// Snapshot opts a case into capturing a per-model golden-baseline
	// response and diffing future runs against it. A detected diff is
	// informational only unless SnapshotStrict is also true.
	Snapshot *bool `yaml:"snapshot,omitempty"`
	// SnapshotStrict makes a detected snapshot diff a hard case failure,
	// same as any other assertion. Meaningless if Snapshot is not true.
	SnapshotStrict *bool `yaml:"snapshot_strict,omitempty"`
	// Fuzz opts a case into robustness testing: deterministic paraphrases
	// of Prompt are generated and each is checked against Triggered. Only
	// meaningful when Triggered is also set — there's nothing for a
	// mutation's outcome to flip against otherwise.
	Fuzz *bool `yaml:"fuzz,omitempty"`
	// FuzzStrict makes a flipped mutation a hard case failure, same as any
	// other assertion. Meaningless if Fuzz is not true.
	FuzzStrict *bool `yaml:"fuzz_strict,omitempty"`
	// FuzzLLM additionally generates paraphrases via the model itself,
	// testing against realistic rewording a fixed synonym dictionary
	// structurally can't anticipate — the failure mode this catches
	// (a skill that only triggers on wording nobody predicted in advance)
	// is different from what Fuzz's deterministic operators test, not a
	// stronger version of the same thing. Generated once per unique
	// prompt and cached to .skillci/fuzz-llm-cache.json, so the API cost
	// and non-determinism are paid once, not on every run. Meaningless if
	// Fuzz is not true — it adds mutations to the same fuzz pass, it
	// doesn't run independently.
	FuzzLLM *bool `yaml:"fuzz_llm,omitempty"`
	// MaxOutputTokens caps the response length. A violation is always a
	// hard case failure, same as MaxTokensLoaded.
	MaxOutputTokens *int `yaml:"max_output_tokens,omitempty"`
	// MaxLatencyMs caps the wall-clock time of the model call. Unlike
	// MaxOutputTokens/MaxCostUSD, a violation is informational only
	// unless LatencyStrict is also true — latency reflects network/
	// inference variance, not skill behavior, so it gets the same
	// non-strict-by-default treatment as Snapshot/Fuzz.
	MaxLatencyMs *int64 `yaml:"max_latency_ms,omitempty"`
	// LatencyStrict makes an exceeded latency cap a hard case failure,
	// same as any other assertion. Meaningless if MaxLatencyMs is not set.
	LatencyStrict *bool `yaml:"latency_strict,omitempty"`
	// MaxCostUSD caps the estimated dollar cost of the call, computed from
	// token counts and the model's pricing entry in .skillci.yaml. A
	// violation is always a hard case failure. A case using this
	// assertion for a model with no pricing entry configured fails with a
	// clear "no pricing configured" message, never silently skipped.
	MaxCostUSD *float64 `yaml:"max_cost_usd,omitempty"`
	// FlakeRetries reruns the case up to N additional times when its
	// trigger-related assertions (Triggered/Contains/NotContains) fail on
	// the first attempt, taking a majority verdict across all attempts
	// instead of trusting a single sample. Budget assertions
	// (MaxTokensLoaded/MaxOutputTokens/MaxLatencyMs/MaxCostUSD) are never
	// retried — they're checked once against the first attempt only, same
	// as today.
	FlakeRetries *int `yaml:"flake_retries,omitempty"`
	// FlakeStrict makes an unresolved tie (no majority reached across all
	// attempts) a hard case failure. Meaningless if FlakeRetries is not
	// set. Without it, a tie is informational only.
	FlakeStrict *bool `yaml:"flake_strict,omitempty"`
	// FlakeAlwaysSample runs the full FlakeRetries vote even when the
	// first attempt already passes its trigger checks, instead of only
	// voting after an initial failure. Without it, a case that's
	// genuinely flaky but happens to pass on attempt 1 is recorded as a
	// clean pass with no vote at all — catching false positives (one
	// unlucky roll) but not false negatives (a real regression that got
	// lucky). Mirrors JudgeSamples, which already always samples
	// unconditionally for the judge layer; this closes the same gap for
	// the case-level trigger checks, at the cost of running the full
	// vote (and its API calls) on every case, not just failing ones.
	// Meaningless if FlakeRetries is not set.
	FlakeAlwaysSample *bool `yaml:"flake_always_sample,omitempty"`
	// Judge lists criteria a separate judge model evaluates against the
	// response, once every other assertion has already passed. All
	// criteria must pass for the judge step itself to pass. Requires
	// JudgeModel to be configured in .skillci.yaml — a case using Judge
	// with no judge_model configured fails with a clear error, never
	// silently skipped.
	Judge []JudgeCriterion `yaml:"judge,omitempty"`
	// JudgeStrict makes a failing criterion a hard case failure. Without
	// it, a judge failure is informational only — the same
	// non-strict-by-default pattern as Snapshot/Fuzz/MaxLatencyMs/
	// FlakeRetries.
	JudgeStrict *bool `yaml:"judge_strict,omitempty"`
	// JudgeMode overrides the global judge_mode (see .skillci.yaml) for
	// this case only: "batched" (every judge criterion in one API call)
	// or "isolated" (one API call per criterion — full reasoning
	// isolation, no cross-criterion bleed, at higher cost). Nil means
	// "use the global default." Meaningless if Judge is not set.
	JudgeMode *string `yaml:"judge_mode,omitempty"`
	// JudgeSamples draws this many independent judge-call samples per
	// criterion group and majority-votes the result, protecting against
	// the judge model's own sampling variance on a borderline verdict —
	// the same asymmetry FlakeRetries closes for the case model. Nil
	// means 1 sample (today's exact behavior: a single call, trusted
	// directly, no voting). Meaningless if Judge is not set.
	JudgeSamples *int `yaml:"judge_samples,omitempty"`
}

// JudgeCriterion is one named rubric item a judge model evaluates a
// response against.
type JudgeCriterion struct {
	Name      string `yaml:"name"`
	Criterion string `yaml:"criterion"`
}

type Case struct {
	Name           string     `yaml:"name"`
	Prompt         string     `yaml:"prompt"`
	SkillUnderTest string     `yaml:"skill_under_test"`
	Assert         Assertions `yaml:"assert"`
	// Dimensions is free-form metadata for slicing regression results —
	// e.g. {"segment": "enterprise", "language": "es"} — with no meaning
	// to skillci itself beyond matching against config.StrictDimensions
	// and grouping in CLI/dashboard output. A case with no dimensions:
	// block behaves exactly as before this field existed.
	Dimensions map[string]string `yaml:"dimensions,omitempty"`
	SourceFile string            `yaml:"-"`
}

// LoadDir reads every *.yaml file directly under evalsDir (not recursively,
// so evals/_generated/*.yaml is excluded by construction — those are
// pending regression-derived cases awaiting `skillci accept`).
func LoadDir(evalsDir string) ([]Case, error) {
	entries, err := os.ReadDir(evalsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cases []Case
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(evalsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var c Case
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		c.SourceFile = path
		cases = append(cases, c)
	}

	// Every feature that looks a case up by name (bisect's target
	// selection, regress's per-(case,model) history tracking, accept)
	// treats Name as a unique key — a duplicate or missing name would
	// otherwise make that lookup silently ambiguous instead of failing
	// loudly here, at load time, where the conflicting files are known.
	seenAt := make(map[string]string, len(cases))
	for _, c := range cases {
		if c.Name == "" {
			return nil, fmt.Errorf("%s: case has no name", c.SourceFile)
		}
		if prevFile, ok := seenAt[c.Name]; ok {
			return nil, fmt.Errorf("duplicate case name %q in %s and %s", c.Name, prevFile, c.SourceFile)
		}
		seenAt[c.Name] = c.SourceFile
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}
