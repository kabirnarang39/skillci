package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
