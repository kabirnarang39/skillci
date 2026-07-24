package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "models: [claude-sonnet-5, claude-opus-4-8]\nfail_on: any_fail\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Models) != 2 || cfg.Models[0] != "claude-sonnet-5" {
		t.Errorf("Models = %v, want [claude-sonnet-5 claude-opus-4-8]", cfg.Models)
	}
	if cfg.FailOn != "any_fail" {
		t.Errorf("FailOn = %q, want any_fail", cfg.FailOn)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	def := Default()
	if cfg.FailOn != def.FailOn || len(cfg.Models) != len(def.Models) {
		t.Errorf("Load(missing) = %+v, want default %+v", cfg, def)
	}
}

func TestLoadParsesPricing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := `models: [claude-sonnet-5]
pricing:
  claude-sonnet-5:
    input_per_million: 3.0
    output_per_million: 15.0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	price, ok := cfg.Pricing["claude-sonnet-5"]
	if !ok {
		t.Fatalf("Pricing[claude-sonnet-5] not found, got %+v", cfg.Pricing)
	}
	if price.InputPerMillion != 3.0 {
		t.Errorf("InputPerMillion = %v, want 3.0", price.InputPerMillion)
	}
	if price.OutputPerMillion != 15.0 {
		t.Errorf("OutputPerMillion = %v, want 15.0", price.OutputPerMillion)
	}
}

func TestLoadPricingDefaultsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Pricing) != 0 {
		t.Errorf("Pricing = %+v, want empty when not configured", cfg.Pricing)
	}
}
