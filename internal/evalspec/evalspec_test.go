package evalspec

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCase(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "triggers.yaml", `
name: triggers-on-pr-review-request
prompt: "Can you review this PR for SOLID violations?"
skill_under_test: pr-review
assert:
  triggered: true
  contains: ["SOLID", "verdict"]
  not_contains: ["I cannot"]
  max_tokens_loaded: 3000
`)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("LoadDir() got %d cases, want 1", len(cases))
	}
	c := cases[0]
	if c.Name != "triggers-on-pr-review-request" || c.SkillUnderTest != "pr-review" {
		t.Errorf("case = %+v, unexpected fields", c)
	}
	if c.Assert.Triggered == nil || !*c.Assert.Triggered {
		t.Errorf("Assert.Triggered = %v, want true", c.Assert.Triggered)
	}
	if len(c.Assert.Contains) != 2 {
		t.Errorf("Assert.Contains = %v, want 2 entries", c.Assert.Contains)
	}
	if c.Assert.MaxTokensLoaded == nil || *c.Assert.MaxTokensLoaded != 3000 {
		t.Errorf("Assert.MaxTokensLoaded = %v, want 3000", c.Assert.MaxTokensLoaded)
	}
}

func TestLoadDirSkipsGeneratedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "real.yaml", "name: real\nprompt: p\nskill_under_test: s\nassert:\n  triggered: true\n")
	genDir := filepath.Join(dir, "_generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCase(t, genDir, "pending.yaml", "name: pending\nprompt: p\nskill_under_test: s\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 || cases[0].Name != "real" {
		t.Errorf("LoadDir() = %v, want only the non-generated case", cases)
	}
}

func TestLoadDirParsesSnapshotFields(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "snap.yaml", `
name: snapshot-case
prompt: "hi"
skill_under_test: some-skill
assert:
  snapshot: true
  snapshot_strict: true
`)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("LoadDir() got %d cases, want 1", len(cases))
	}
	c := cases[0]
	if c.Assert.Snapshot == nil || !*c.Assert.Snapshot {
		t.Errorf("Assert.Snapshot = %v, want true", c.Assert.Snapshot)
	}
	if c.Assert.SnapshotStrict == nil || !*c.Assert.SnapshotStrict {
		t.Errorf("Assert.SnapshotStrict = %v, want true", c.Assert.SnapshotStrict)
	}
}

func TestLoadDirSnapshotFieldsDefaultNil(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "plain.yaml", "name: plain-case\nprompt: p\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Assert.Snapshot != nil {
		t.Errorf("Assert.Snapshot = %v, want nil when not specified", c.Assert.Snapshot)
	}
	if c.Assert.SnapshotStrict != nil {
		t.Errorf("Assert.SnapshotStrict = %v, want nil when not specified", c.Assert.SnapshotStrict)
	}
}

func TestLoadDirParsesFuzzFields(t *testing.T) {
	dir := t.TempDir()
	content := "name: fuzz-case\nprompt: hi\nassert:\n  triggered: true\n  fuzz: true\n  fuzz_strict: true\n"
	if err := os.WriteFile(filepath.Join(dir, "case.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	c := cases[0]
	if c.Assert.Fuzz == nil || !*c.Assert.Fuzz {
		t.Error("Assert.Fuzz = nil or false, want true")
	}
	if c.Assert.FuzzStrict == nil || !*c.Assert.FuzzStrict {
		t.Error("Assert.FuzzStrict = nil or false, want true")
	}
}

func TestLoadDirParsesCostLatencyFields(t *testing.T) {
	dir := t.TempDir()
	content := "name: cost-case\nprompt: hi\nassert:\n  triggered: true\n  max_output_tokens: 500\n  max_latency_ms: 3000\n  latency_strict: true\n  max_cost_usd: 0.05\n"
	writeCase(t, dir, "case.yaml", content)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	c := cases[0]
	if c.Assert.MaxOutputTokens == nil || *c.Assert.MaxOutputTokens != 500 {
		t.Errorf("MaxOutputTokens = %v, want 500", c.Assert.MaxOutputTokens)
	}
	if c.Assert.MaxLatencyMs == nil || *c.Assert.MaxLatencyMs != 3000 {
		t.Errorf("MaxLatencyMs = %v, want 3000", c.Assert.MaxLatencyMs)
	}
	if c.Assert.LatencyStrict == nil || !*c.Assert.LatencyStrict {
		t.Errorf("LatencyStrict = %v, want true", c.Assert.LatencyStrict)
	}
	if c.Assert.MaxCostUSD == nil || *c.Assert.MaxCostUSD != 0.05 {
		t.Errorf("MaxCostUSD = %v, want 0.05", c.Assert.MaxCostUSD)
	}
}

func TestLoadDirCostLatencyFieldsDefaultNil(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "plain.yaml", "name: plain-case\nprompt: p\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Assert.MaxOutputTokens != nil {
		t.Errorf("MaxOutputTokens = %v, want nil when not specified", c.Assert.MaxOutputTokens)
	}
	if c.Assert.MaxLatencyMs != nil {
		t.Errorf("MaxLatencyMs = %v, want nil when not specified", c.Assert.MaxLatencyMs)
	}
	if c.Assert.LatencyStrict != nil {
		t.Errorf("LatencyStrict = %v, want nil when not specified", c.Assert.LatencyStrict)
	}
	if c.Assert.MaxCostUSD != nil {
		t.Errorf("MaxCostUSD = %v, want nil when not specified", c.Assert.MaxCostUSD)
	}
}

func TestLoadDirParsesDimensions(t *testing.T) {
	dir := t.TempDir()
	content := "name: enterprise-case\nprompt: hi\nassert:\n  triggered: true\ndimensions:\n  segment: enterprise\n  language: es\n"
	writeCase(t, dir, "case.yaml", content)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	c := cases[0]
	if c.Dimensions["segment"] != "enterprise" {
		t.Errorf("Dimensions[segment] = %q, want enterprise", c.Dimensions["segment"])
	}
	if c.Dimensions["language"] != "es" {
		t.Errorf("Dimensions[language] = %q, want es", c.Dimensions["language"])
	}
}

func TestLoadDirDimensionsDefaultNil(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "plain.yaml", "name: plain-case\nprompt: p\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Dimensions != nil {
		t.Errorf("Dimensions = %v, want nil when not specified", c.Dimensions)
	}
}
