package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestLoadRejectsUnrecognizedFailOn covers a typo'd or otherwise invalid
// fail_on value (e.g. "any_fial", or a value from an older/newer version
// of the tool the user misremembers). Without validation, ShouldFailCI's
// switch silently falls through to its "regression" default for any
// unrecognized string — so a user who thought they configured any_fail
// (fail on every failing case) gets the loosest policy instead, with
// nothing telling them their config was ignored. This must be a load-time
// error, not a silent fallback.
func TestLoadRejectsUnrecognizedFailOn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "fail_on: any_fial\n" // typo of any_fail
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for an unrecognized fail_on value")
	}
	if !strings.Contains(err.Error(), "any_fial") {
		t.Errorf("Load() error = %q, want it to name the invalid value", err)
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

func TestLoadParsesJudgeModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "models: [claude-sonnet-5]\njudge_model: claude-opus-4-8\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JudgeModel != "claude-opus-4-8" {
		t.Errorf("JudgeModel = %q, want claude-opus-4-8", cfg.JudgeModel)
	}
}

func TestLoadJudgeModelDefaultsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JudgeModel != "" {
		t.Errorf("JudgeModel = %q, want empty when not configured", cfg.JudgeModel)
	}
}

func TestLoadParsesJudgeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "models: [claude-sonnet-5]\njudge_mode: isolated\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JudgeMode != "isolated" {
		t.Errorf("JudgeMode = %q, want isolated", cfg.JudgeMode)
	}
}

func TestLoadJudgeModeDefaultsToBatched(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JudgeMode != "batched" {
		t.Errorf("JudgeMode = %q, want batched when not configured", cfg.JudgeMode)
	}
}

func TestLoadRejectsUnrecognizedJudgeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "models: [claude-sonnet-5]\njudge_mode: sometimes\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("Load() error = nil, want an error for an unrecognized judge_mode value")
	}
}

// TestLoadRejectsYAMLAliasBombQuickly covers a "billion laughs"-style YAML
// alias-expansion attack in .skillci.yaml. yaml.v3 has its own built-in
// alias-count limit; this locks in that skillci's own error path surfaces
// it cleanly and quickly rather than hanging or OOMing — run with a hard
// deadline so a real regression here fails the test instead of hanging
// the whole suite.
func TestLoadRejectsYAMLAliasBombQuickly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	bomb := "a0: &a0 [\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\"]\n"
	for i := 1; i < 10; i++ {
		bomb += fmt.Sprintf("a%d: &a%d [*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d]\n", i, i, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1)
	}
	bomb += "models: [*a9]\n"
	if err := os.WriteFile(path, []byte(bomb), 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Load(path)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Load() error = nil, want an error rejecting the alias bomb")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load() did not return within 5s — alias-bomb protection regressed")
	}
}
