package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeReportTestSkill(t *testing.T, dir string) {
	t.Helper()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReportCommandRejectsMissingComplianceFlag(t *testing.T) {
	dir := t.TempDir()
	writeReportTestSkill(t, dir)

	cmd := newReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want an error when --compliance is not set")
	}
}

func TestReportCommandRejectsUnknownComplianceValue(t *testing.T) {
	dir := t.TempDir()
	writeReportTestSkill(t, dir)

	cmd := newReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--compliance", "sox", dir})

	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want an error for an unrecognized --compliance value")
	}
}

// TestReportCommandNISTAIRMFEndToEnd is the reachability test for the real
// command: a skill with a real eval case must produce a report containing
// that case's own data, not just static boilerplate.
func TestReportCommandNISTAIRMFEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeReportTestSkill(t, dir)
	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseYAML := "name: \"case-1\"\nprompt: \"hi\"\nskill_under_test: \"my-skill\"\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "case1.yaml"), []byte(caseYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--compliance", "nist-ai-rmf", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("1 eval case(s)")) {
		t.Errorf("output = %q, want it to report the real eval case count", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("1 case(s) have an explicit `triggered`")) {
		t.Errorf("output = %q, want it to report the real triggered-case count", out.String())
	}
}

// TestReportCommandEUAIActReflectsConfigRetention proves the real command
// (not just the internal/compliance package in isolation) picks up
// .skillci.yaml's history_retention_runs and threads it through to the
// rendered report.
func TestReportCommandEUAIActReflectsConfigRetention(t *testing.T) {
	dir := t.TempDir()
	writeReportTestSkill(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".skillci.yaml"), []byte("history_retention_runs: 50\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--compliance", "eu-ai-act", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("50 run(s)")) {
		t.Errorf("output = %q, want it to reflect .skillci.yaml's history_retention_runs: 50", out.String())
	}
}
