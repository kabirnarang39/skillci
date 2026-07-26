package config

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Models []string `yaml:"models"`
	FailOn string   `yaml:"fail_on"`
	// Pricing is always user-supplied — skillci never hardcodes or
	// maintains a per-model price table, since Anthropic can reprice
	// without notice and a stale table would silently misreport cost.
	// A model with no entry here simply can't use max_cost_usd.
	Pricing map[string]ModelPricing `yaml:"pricing"`
	// StrictDimensions names eval-case Dimensions key/values that always
	// fail CI on any case failure, regardless of FailOn — e.g.
	// {"segment": ["enterprise"]} means any case with
	// Dimensions["segment"] == "enterprise" gates CI strictly even if
	// FailOn is set to a looser policy like "triggered_only".
	StrictDimensions map[string][]string `yaml:"strict_dimensions"`
	// JudgeModel is the model that evaluates Judge criteria —
	// deliberately separate from the models under test, since a model
	// can't reliably judge itself when it's also the thing that might
	// have drifted. A case with no Judge criteria doesn't need this set.
	JudgeModel string `yaml:"judge_model"`
	// JudgeMode sets the default judge call grouping: "batched" (every
	// judge criterion in one API call, the existing cost profile) or
	// "isolated" (one API call per criterion). A case's own
	// assert.judge_mode overrides this. Unset defaults to "batched".
	JudgeMode string `yaml:"judge_mode"`
	// Lint controls `skillci check`'s severity/blocking behavior — lets a
	// platform team pilot skillci non-blocking in CI before promoting
	// specific rules (or everything) to hard-fail, instead of an
	// all-or-nothing cutover the first time it's wired in.
	Lint LintPolicy `yaml:"lint"`
}

// LintPolicy is `skillci check`'s severity/blocking configuration.
type LintPolicy struct {
	// Mode is the default severity for any rule not named in Rules:
	// "block" (default — a lint issue fails the command, the long-standing
	// behavior) or "warn" (issues are still reported but never fail the
	// command). check's own --mode flag, when passed, overrides this
	// entirely — including Rules — for a quick one-off without editing
	// the config file.
	Mode string `yaml:"mode"`
	// Rules overrides Mode on a per-rule-name basis, e.g.
	// {"missing-description": "warn"} lets a team promote specific rules
	// to blocking while piloting others as warnings.
	Rules map[string]string `yaml:"rules"`
}

// Severity resolves the effective severity ("block" or "warn") for a lint
// rule: an entry in Rules wins, falling back to Mode, falling back to
// "block" — the behavior an empty/absent LintPolicy has always had.
func (lp LintPolicy) Severity(rule string) string {
	if s, ok := lp.Rules[rule]; ok {
		return s
	}
	if lp.Mode != "" {
		return lp.Mode
	}
	return "block"
}

// ModelPricing is one model's per-million-token rates, matching
// Anthropic's own pricing convention.
type ModelPricing struct {
	InputPerMillion  float64 `yaml:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million"`
}

// validFailOn is the exact set ShouldFailCI's switch (internal/regress)
// recognizes. Kept in sync by hand — ShouldFailCI's default case silently
// treats anything else as "regression", so an unrecognized value here must
// fail loudly at load time instead of silently getting the loosest policy.
var validFailOn = map[string]bool{
	"regression":     true,
	"any_fail":       true,
	"triggered_only": true,
}

const validFailOnList = `"regression", "any_fail", "triggered_only"`

// validJudgeMode is the exact set RunCase's mode-resolution logic
// (internal/runner) recognizes for both the global judge_mode and a
// case's own assert.judge_mode override.
var validJudgeMode = map[string]bool{
	"batched":  true,
	"isolated": true,
}

const validJudgeModeList = `"batched", "isolated"`

// validSeverity is the exact set LintPolicy.Severity can resolve to.
var validSeverity = map[string]bool{
	"block": true,
	"warn":  true,
}

const validSeverityList = `"block", "warn"`

func Default() Config {
	return Config{
		Models:    []string{"claude-sonnet-5"},
		FailOn:    "regression",
		JudgeMode: "batched",
	}
}

// Load reads a .skillci.yaml file at path. A missing file is not an error —
// it returns Default() so `skillci check`/`eval` work with zero config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if len(cfg.Models) == 0 {
		cfg.Models = Default().Models
	}
	if cfg.FailOn == "" {
		cfg.FailOn = Default().FailOn
	}
	if _, ok := validFailOn[cfg.FailOn]; !ok {
		return Config{}, fmt.Errorf("fail_on: %q is not a recognized value — must be one of %s", cfg.FailOn, validFailOnList)
	}
	if cfg.JudgeMode == "" {
		cfg.JudgeMode = "batched"
	}
	if !validJudgeMode[cfg.JudgeMode] {
		return Config{}, fmt.Errorf("judge_mode: %q is not a recognized value — must be one of %s", cfg.JudgeMode, validJudgeModeList)
	}
	if cfg.Lint.Mode != "" && !validSeverity[cfg.Lint.Mode] {
		return Config{}, fmt.Errorf("lint.mode: %q is not a recognized value — must be one of %s", cfg.Lint.Mode, validSeverityList)
	}
	// Sorted iteration for the same reason as the Pricing loop below: a
	// deterministic error on every run when multiple entries are invalid.
	rules := make([]string, 0, len(cfg.Lint.Rules))
	for rule := range cfg.Lint.Rules {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	for _, rule := range rules {
		if severity := cfg.Lint.Rules[rule]; !validSeverity[severity] {
			return Config{}, fmt.Errorf("lint.rules[%q]: %q is not a recognized value — must be one of %s", rule, severity, validSeverityList)
		}
	}
	// Sorted iteration: map order is nondeterministic in Go, and if
	// multiple pricing entries are invalid at once the error should
	// always name the same one on every run, not an arbitrary one.
	models := make([]string, 0, len(cfg.Pricing))
	for model := range cfg.Pricing {
		models = append(models, model)
	}
	sort.Strings(models)
	for _, model := range models {
		price := cfg.Pricing[model]
		if price.InputPerMillion <= 0 || price.OutputPerMillion <= 0 {
			return Config{}, fmt.Errorf("pricing entry for %q is incomplete: input_per_million and output_per_million must both be > 0 (got %v/%v)", model, price.InputPerMillion, price.OutputPerMillion)
		}
	}
	return cfg, nil
}
