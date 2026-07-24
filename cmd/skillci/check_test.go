package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
