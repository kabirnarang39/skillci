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
}

type Case struct {
	Name           string     `yaml:"name"`
	Prompt         string     `yaml:"prompt"`
	SkillUnderTest string     `yaml:"skill_under_test"`
	Assert         Assertions `yaml:"assert"`
	SourceFile     string     `yaml:"-"`
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
