package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
