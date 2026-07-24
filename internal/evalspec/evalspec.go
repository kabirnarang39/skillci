package evalspec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Assertions struct {
	Triggered       *bool    `yaml:"triggered"`
	Contains        []string `yaml:"contains"`
	NotContains     []string `yaml:"not_contains"`
	MaxTokensLoaded *int     `yaml:"max_tokens_loaded"`
	// Snapshot opts a case into capturing a per-model golden-baseline
	// response and diffing future runs against it. A detected diff is
	// informational only unless SnapshotStrict is also true.
	Snapshot *bool `yaml:"snapshot"`
	// SnapshotStrict makes a detected snapshot diff a hard case failure,
	// same as any other assertion. Meaningless if Snapshot is not true.
	SnapshotStrict *bool `yaml:"snapshot_strict"`
	// Fuzz opts a case into robustness testing: deterministic paraphrases
	// of Prompt are generated and each is checked against Triggered. Only
	// meaningful when Triggered is also set — there's nothing for a
	// mutation's outcome to flip against otherwise.
	Fuzz *bool `yaml:"fuzz"`
	// FuzzStrict makes a flipped mutation a hard case failure, same as any
	// other assertion. Meaningless if Fuzz is not true.
	FuzzStrict *bool `yaml:"fuzz_strict"`
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
	FuzzLLM *bool `yaml:"fuzz_llm"`
	// MaxOutputTokens caps the response length. A violation is always a
	// hard case failure, same as MaxTokensLoaded.
	MaxOutputTokens *int `yaml:"max_output_tokens"`
	// MaxLatencyMs caps the wall-clock time of the model call. Unlike
	// MaxOutputTokens/MaxCostUSD, a violation is informational only
	// unless LatencyStrict is also true — latency reflects network/
	// inference variance, not skill behavior, so it gets the same
	// non-strict-by-default treatment as Snapshot/Fuzz.
	MaxLatencyMs *int64 `yaml:"max_latency_ms"`
	// LatencyStrict makes an exceeded latency cap a hard case failure,
	// same as any other assertion. Meaningless if MaxLatencyMs is not set.
	LatencyStrict *bool `yaml:"latency_strict"`
	// MaxCostUSD caps the estimated dollar cost of the call, computed from
	// token counts and the model's pricing entry in .skillci.yaml. A
	// violation is always a hard case failure. A case using this
	// assertion for a model with no pricing entry configured fails with a
	// clear "no pricing configured" message, never silently skipped.
	MaxCostUSD *float64 `yaml:"max_cost_usd"`
	// FlakeRetries reruns the case up to N additional times when its
	// trigger-related assertions (Triggered/Contains/NotContains) fail on
	// the first attempt, taking a majority verdict across all attempts
	// instead of trusting a single sample. Budget assertions
	// (MaxTokensLoaded/MaxOutputTokens/MaxLatencyMs/MaxCostUSD) are never
	// retried — they're checked once against the first attempt only, same
	// as today.
	FlakeRetries *int `yaml:"flake_retries"`
	// FlakeStrict makes an unresolved tie (no majority reached across all
	// attempts) a hard case failure. Meaningless if FlakeRetries is not
	// set. Without it, a tie is informational only.
	FlakeStrict *bool `yaml:"flake_strict"`
	// Judge lists criteria a separate judge model evaluates against the
	// response, once every other assertion has already passed. All
	// criteria must pass for the judge step itself to pass. Requires
	// JudgeModel to be configured in .skillci.yaml — a case using Judge
	// with no judge_model configured fails with a clear error, never
	// silently skipped.
	Judge []JudgeCriterion `yaml:"judge"`
	// JudgeStrict makes a failing criterion a hard case failure. Without
	// it, a judge failure is informational only — the same
	// non-strict-by-default pattern as Snapshot/Fuzz/MaxLatencyMs/
	// FlakeRetries.
	JudgeStrict *bool `yaml:"judge_strict"`
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
	Dimensions map[string]string `yaml:"dimensions"`
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
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}
