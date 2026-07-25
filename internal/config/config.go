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

func Default() Config {
	return Config{
		Models: []string{"claude-sonnet-5"},
		FailOn: "regression",
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
