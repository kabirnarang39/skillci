package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadRejectsIncompletePricingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := `models: [claude-sonnet-5]
pricing:
  claude-sonnet-5:
    input_per_million: 3.0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for incomplete pricing entry (missing output_per_million)")
	}
	if !strings.Contains(err.Error(), "claude-sonnet-5") {
		t.Errorf("Load() error = %q, want it to mention the model name", err.Error())
	}
}

func TestLoadRejectsZeroPricingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := `models: [claude-sonnet-5]
pricing:
  claude-sonnet-5:
    input_per_million: 0
    output_per_million: 0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for a fully-zero pricing entry")
	}
	if !strings.Contains(err.Error(), "claude-sonnet-5") {
		t.Errorf("Load() error = %q, want it to mention the model name", err.Error())
	}
}

func TestLoadRejectsMultipleInvalidPricingEntriesDeterministically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := `models: [claude-sonnet-5]
pricing:
  zzz-model:
    input_per_million: 0
    output_per_million: 0
  aaa-model:
    input_per_million: 0
    output_per_million: 0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for invalid pricing entries")
		}
		// Sorted iteration means "aaa-model" is always named first,
		// never "zzz-model" — map order must not leak into the error.
		if !strings.Contains(err.Error(), "aaa-model") {
			t.Fatalf("run %d: Load() error = %q, want it to name aaa-model (the alphabetically-first invalid entry), not an arbitrary one", i, err.Error())
		}
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

func TestLoadParsesStrictDimensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := `models: [claude-sonnet-5]
fail_on: triggered_only
strict_dimensions:
  segment: [enterprise, government]
  language: [es]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.StrictDimensions["segment"]) != 2 || cfg.StrictDimensions["segment"][0] != "enterprise" {
		t.Errorf("StrictDimensions[segment] = %v, want [enterprise government]", cfg.StrictDimensions["segment"])
	}
	if len(cfg.StrictDimensions["language"]) != 1 || cfg.StrictDimensions["language"][0] != "es" {
		t.Errorf("StrictDimensions[language] = %v, want [es]", cfg.StrictDimensions["language"])
	}
}

func TestLoadStrictDimensionsDefaultsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.StrictDimensions) != 0 {
		t.Errorf("StrictDimensions = %+v, want empty when not configured", cfg.StrictDimensions)
	}
}
