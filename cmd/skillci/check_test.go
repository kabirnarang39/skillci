package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/lint"
)

func TestCheckCommandReportsIssues(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	err := cmd.Execute()
	if err == nil {
		t.Error("Execute() error = nil, want error because of lint issues (missing description)")
	}
}

func TestCheckCommandPassesCleanSkill(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil for a clean skill", err)
	}
}

// TestCheckCommandJSONFormatEmitsParseableIssues is the reachability test
// for --format json: proves the real command's output is genuinely valid,
// parseable JSON matching lint.Issue's shape — the actual contract an
// editor extension or script depends on — not just that the flag exists.
func TestCheckCommandJSONFormatEmitsParseableIssues(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n" // missing description -> one issue
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--format", "json", dir})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error because of lint issues (missing description)")
	}

	var issues []lint.Issue
	if err := json.Unmarshal(out.Bytes(), &issues); err != nil {
		t.Fatalf("output is not valid JSON: %v; output = %s", err, out.String())
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "missing-description" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want a missing-description issue", issues)
	}
}

// TestCheckCommandJSONFormatEmitsEmptyArrayNotNullOnCleanSkill proves a
// clean skill emits `[]`, not JSON `null` — a nil Go slice marshals to
// null, which would force every machine reader (an editor extension, a
// script) to special-case "no issues" as a distinct shape from "some
// issues", instead of always getting a JSON array.
func TestCheckCommandJSONFormatEmitsEmptyArrayNotNullOnCleanSkill(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--format", "json", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil for a clean skill", err)
	}

	trimmed := bytes.TrimSpace(out.Bytes())
	if string(trimmed) != "[]" {
		t.Errorf("output = %q, want exactly \"[]\" for a clean skill", trimmed)
	}
}

func TestCheckCommandRejectsInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--format", "yaml", dir})

	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want error for an unsupported --format value")
	}
}

// TestCheckCommandVerifyPinnedSourcesFlagsRealMismatch is the end-to-end
// reachability test for --verify-pinned-sources: runs the real check
// command against a real local HTTP server and confirms a hash mismatch
// actually reaches the command's own output, not just that
// lint.VerifyPinnedSources behaves correctly in isolation.
func TestCheckCommandVerifyPinnedSourcesFlagsRealMismatch(t *testing.T) {
	origDialer := lint.PinnedSourceDialer
	lint.PinnedSourceDialer = (&net.Dialer{}).DialContext
	t.Cleanup(func() { lint.PinnedSourceDialer = origDialer })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "content has drifted since it was pinned")
	}))
	defer srv.Close()

	originalHash := sha256.Sum256([]byte("the original pinned content"))
	dir := t.TempDir()
	content := fmt.Sprintf("---\nname: my-skill\ndescription: Does a thing.\npinned_sources:\n  - url: %s\n    sha256: %s\n---\nBody.\n", srv.URL, hex.EncodeToString(originalHash[:]))
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--verify-pinned-sources", dir})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error because the pinned source's hash has changed")
	}
	if !strings.Contains(out.String(), "ast02-pinned-source-mismatch") {
		t.Errorf("output = %q, want an ast02-pinned-source-mismatch issue", out.String())
	}
}

// TestCheckCommandModeWarnDoesNotFailDespiteIssues is the reachability test
// for --mode=warn: a real skill with a real lint issue must still exit 0,
// and the output must say so instead of looking like a silent, unexplained
// pass — the whole point of a gradual-adoption mode is that findings stay
// visible without blocking the build.
func TestCheckCommandModeWarnDoesNotFailDespiteIssues(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n" // missing description -> one issue
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--mode", "warn", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil — --mode=warn must never fail the command", err)
	}
	if !strings.Contains(out.String(), "missing-description") {
		t.Errorf("output = %q, want the issue still reported", out.String())
	}
	if !strings.Contains(out.String(), "warn-only") {
		t.Errorf("output = %q, want a warn-only summary so the pass isn't silent", out.String())
	}
}

// TestCheckCommandRejectsInvalidMode covers a typo'd --mode value the same
// way TestCheckCommandRejectsInvalidFormat covers --format.
func TestCheckCommandRejectsInvalidMode(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--mode", "yolo", dir})

	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want an error for an unsupported --mode value")
	}
}

// TestCheckCommandConfigLintModeWarnMatchesFlag proves .skillci.yaml's
// lint.mode achieves the exact same non-blocking behavior as --mode=warn,
// without requiring the flag — the config-file path a CI YAML wouldn't
// need to change to pilot skillci non-blocking.
func TestCheckCommandConfigLintModeWarnMatchesFlag(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".skillci.yaml"), []byte("lint:\n  mode: warn\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil — lint.mode: warn in .skillci.yaml must never fail the command", err)
	}
}

// TestCheckCommandModeFlagOverridesConfigRules proves --mode is a true
// global override: even a .skillci.yaml that promotes a specific rule to
// "block" must not fail the command once --mode=warn is passed on the CLI.
func TestCheckCommandModeFlagOverridesConfigRules(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".skillci.yaml"), []byte("lint:\n  rules:\n    missing-description: block\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--mode", "warn", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil — --mode=warn must override even a per-rule block in config", err)
	}
}

// TestCheckCommandConfigRuleOverridePromotesSpecificRuleToBlock proves the
// per-rule override direction too: a rule explicitly set to "block" in
// .skillci.yaml must still fail the command even when lint.mode is warn —
// the exact mechanism a team promoting one rule while piloting the rest
// depends on.
func TestCheckCommandConfigRuleOverridePromotesSpecificRuleToBlock(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n" // missing-description issue
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".skillci.yaml"), []byte("lint:\n  mode: warn\n  rules:\n    missing-description: block\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want an error — missing-description is promoted to block despite the warn default")
	}
}

// TestCheckCommandJSONFormatIncludesSeverity proves --format json's new
// severity field actually reaches real output, and that it's additive —
// existing rule/file/line/msg fields must still round-trip through
// lint.Issue the way TestCheckCommandJSONFormatEmitsParseableIssues checks.
func TestCheckCommandJSONFormatIncludesSeverity(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--format", "json", "--mode", "warn", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil under --mode=warn", err)
	}

	var reported []struct {
		lint.Issue
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(out.Bytes(), &reported); err != nil {
		t.Fatalf("output is not valid JSON: %v; output = %s", err, out.String())
	}
	if len(reported) == 0 {
		t.Fatal("reported issues = [], want at least one (missing-description)")
	}
	if reported[0].Severity != "warn" {
		t.Errorf("Severity = %q, want warn", reported[0].Severity)
	}
}

// TestCheckCommandWithoutVerifyPinnedSourcesMakesNoNetworkCall proves the
// flag is genuinely opt-in: a skill with a pinned_sources entry pointing
// at a server that would fail the check must NOT be flagged (and the
// server must never even be hit) when --verify-pinned-sources isn't
// passed — the network call must never happen implicitly.
func TestCheckCommandWithoutVerifyPinnedSourcesMakesNoNetworkCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		fmt.Fprint(w, "irrelevant")
	}))
	defer srv.Close()

	dir := t.TempDir()
	content := fmt.Sprintf("---\nname: my-skill\ndescription: Does a thing.\npinned_sources:\n  - url: %s\n    sha256: deadbeef\n---\nBody.\n", srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir}) // no --verify-pinned-sources

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil — an unverified pinned_sources entry isn't itself an issue", err)
	}
	if called {
		t.Error("the pinned source's server was hit despite --verify-pinned-sources not being passed — this must never make a network call implicitly")
	}
}
