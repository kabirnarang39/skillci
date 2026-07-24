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
}

// ModelPricing is one model's per-million-token rates, matching
// Anthropic's own pricing convention.
type ModelPricing struct {
	InputPerMillion  float64 `yaml:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million"`
}

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
