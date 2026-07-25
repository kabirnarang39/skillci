package evalspec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestLoadDirRejectsDuplicateCaseNames covers two different eval files
// under evals/ that happen to declare the same case name — an easy real
// mistake (e.g. copy-pasting an existing case file and forgetting to
// rename it). Without validation, every feature that looks a case up by
// name (bisect's target selection, regress's per-(case,model) history
// tracking, accept) becomes ambiguous between the two, silently: whichever
// one sorts first wins with no signal that the other is being ignored.
func TestLoadDirRejectsDuplicateCaseNames(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "a.yaml", "name: shared-name\nprompt: \"first\"\n")
	writeCase(t, dir, "b.yaml", "name: shared-name\nprompt: \"second\"\n")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir() error = nil, want an error for duplicate case names across files")
	}
	if !strings.Contains(err.Error(), "shared-name") {
		t.Errorf("LoadDir() error = %q, want it to name the duplicate case", err)
	}
}

func TestLoadDirRejectsEmptyCaseName(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "unnamed.yaml", "prompt: \"no name field\"\n")

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir() error = nil, want an error for a case with no name")
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

func TestLoadDirParsesFlakeFields(t *testing.T) {
	dir := t.TempDir()
	content := "name: flake-case\nprompt: hi\nassert:\n  triggered: true\n  flake_retries: 2\n  flake_strict: true\n"
	writeCase(t, dir, "case.yaml", content)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	c := cases[0]
	if c.Assert.FlakeRetries == nil || *c.Assert.FlakeRetries != 2 {
		t.Errorf("FlakeRetries = %v, want 2", c.Assert.FlakeRetries)
	}
	if c.Assert.FlakeStrict == nil || !*c.Assert.FlakeStrict {
		t.Errorf("FlakeStrict = %v, want true", c.Assert.FlakeStrict)
	}
}

func TestLoadDirFlakeFieldsDefaultNil(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "plain.yaml", "name: plain-case\nprompt: p\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Assert.FlakeRetries != nil {
		t.Errorf("FlakeRetries = %v, want nil when not specified", c.Assert.FlakeRetries)
	}
	if c.Assert.FlakeStrict != nil {
		t.Errorf("FlakeStrict = %v, want nil when not specified", c.Assert.FlakeStrict)
	}
}

func TestLoadDirParsesJudgeFields(t *testing.T) {
	dir := t.TempDir()
	content := `name: judge-case
prompt: hi
assert:
  triggered: true
  judge_strict: true
  judge:
    - name: tone
      criterion: "Is the response friendly and professional?"
    - name: length
      criterion: "Is the response under 100 words?"
`
	writeCase(t, dir, "case.yaml", content)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	c := cases[0]
	if len(c.Assert.Judge) != 2 {
		t.Fatalf("Judge = %v, want 2 criteria", c.Assert.Judge)
	}
	if c.Assert.Judge[0].Name != "tone" || c.Assert.Judge[0].Criterion != "Is the response friendly and professional?" {
		t.Errorf("Judge[0] = %+v, unexpected fields", c.Assert.Judge[0])
	}
	if c.Assert.Judge[1].Name != "length" {
		t.Errorf("Judge[1].Name = %q, want length", c.Assert.Judge[1].Name)
	}
	if c.Assert.JudgeStrict == nil || !*c.Assert.JudgeStrict {
		t.Errorf("JudgeStrict = %v, want true", c.Assert.JudgeStrict)
	}
}

func TestLoadDirJudgeFieldsDefaultNil(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "plain.yaml", "name: plain-case\nprompt: p\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Assert.Judge != nil {
		t.Errorf("Judge = %v, want nil when not specified", c.Assert.Judge)
	}
	if c.Assert.JudgeStrict != nil {
		t.Errorf("JudgeStrict = %v, want nil when not specified", c.Assert.JudgeStrict)
	}
}

func TestLoadDirParsesJudgeModeAndSamples(t *testing.T) {
	dir := t.TempDir()
	content := `name: judge-case
prompt: hi
assert:
  triggered: true
  judge_mode: isolated
  judge_samples: 3
  judge:
    - name: tone
      criterion: "Is the response friendly?"
`
	writeCase(t, dir, "case.yaml", content)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Assert.JudgeMode == nil || *c.Assert.JudgeMode != "isolated" {
		t.Errorf("JudgeMode = %v, want \"isolated\"", c.Assert.JudgeMode)
	}
	if c.Assert.JudgeSamples == nil || *c.Assert.JudgeSamples != 3 {
		t.Errorf("JudgeSamples = %v, want 3", c.Assert.JudgeSamples)
	}
}

func TestLoadDirJudgeModeAndSamplesDefaultNil(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "plain.yaml", "name: plain-case\nprompt: p\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	c := cases[0]
	if c.Assert.JudgeMode != nil {
		t.Errorf("JudgeMode = %v, want nil when not specified", c.Assert.JudgeMode)
	}
	if c.Assert.JudgeSamples != nil {
		t.Errorf("JudgeSamples = %v, want nil when not specified", c.Assert.JudgeSamples)
	}
}

// TestLoadDirRejectsYAMLAliasBombQuickly covers a "billion laughs"-style
// YAML alias-expansion attack (nested anchors/aliases that expand
// exponentially) — genuinely untrusted-ish content, since evals/*.yaml can
// be edited by anyone with repo write access. yaml.v3 has its own
// built-in alias-count limit, but this locks in that skillci's own error
// path surfaces it cleanly and quickly rather than hanging or OOMing —
// run with a hard deadline so a real regression here fails the test
// instead of hanging the whole suite.
func TestLoadDirRejectsYAMLAliasBombQuickly(t *testing.T) {
	dir := t.TempDir()
	bomb := "a0: &a0 [\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\"]\n"
	for i := 1; i < 10; i++ {
		bomb += fmt.Sprintf("a%d: &a%d [*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d,*a%d]\n", i, i, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1)
	}
	bomb += "name: *a9\nprompt: p\n"
	writeCase(t, dir, "bomb.yaml", bomb)

	done := make(chan error, 1)
	go func() {
		_, err := LoadDir(dir)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("LoadDir() error = nil, want an error rejecting the alias bomb")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadDir() did not return within 5s — alias-bomb protection regressed")
	}
}
