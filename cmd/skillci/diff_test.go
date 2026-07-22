package main

import (
	"bytes"
	"testing"

	"github.com/kabirnarang39/skillci/internal/snapshot"
)

func TestDiffCommandShowsPendingChange(t *testing.T) {
	dir := t.TempDir()
	if err := snapshot.Save(dir, "my-case", "claude-sonnet-5", "Old leaves drift and fall."); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.SavePending(dir, "my-case", "claude-sonnet-5", "Old leaves drift and settle."); err != nil {
		t.Fatal(err)
	}

	cmd := newDiffCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"my-case", "--model", "claude-sonnet-5", "--path", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.String() == "" {
		t.Error("output is empty, want a rendered diff")
	}
}

func TestDiffCommandNoPendingChange(t *testing.T) {
	dir := t.TempDir()
	if err := snapshot.Save(dir, "my-case", "claude-sonnet-5", "unchanged text"); err != nil {
		t.Fatal(err)
	}

	cmd := newDiffCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"my-case", "--model", "claude-sonnet-5", "--path", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("no pending")) {
		t.Errorf("output = %q, want a message indicating no pending change", out.String())
	}
}

func TestDiffCommandNoGoldenYet(t *testing.T) {
	dir := t.TempDir()
	cmd := newDiffCmd()
	cmd.SetArgs([]string{"no-such-case", "--model", "claude-sonnet-5", "--path", dir})

	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want error when no golden baseline exists yet")
	}
}
