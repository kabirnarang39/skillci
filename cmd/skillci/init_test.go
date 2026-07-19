package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCommandScaffolds(t *testing.T) {
	dir := t.TempDir()

	cmd := newInitCmd()
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".skillci.yaml")); err != nil {
		t.Errorf(".skillci.yaml not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evals")); err != nil {
		t.Errorf("evals/ not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evals", "example.yaml")); err != nil {
		t.Errorf("evals/example.yaml not created: %v", err)
	}
}

func TestInitCommandDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	existing := "models: [claude-opus-4-8]\n"
	if err := os.WriteFile(filepath.Join(dir, ".skillci.yaml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want error when .skillci.yaml already exists")
	}

	data, err := os.ReadFile(filepath.Join(dir, ".skillci.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Error(".skillci.yaml was overwritten, want it untouched")
	}
}
