package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/gitutil"
	"github.com/kabirnarang39/skillci/internal/history"
)

func runGitB(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupBisectRepo creates a 4-commit git history for a skill living at the
// repo root: c0 (good) has no "BROKEN" marker, c1 also has none, c2
// introduces "BROKEN" in the description (the culprit), c3 keeps it (bad).
// The eval case file is written but intentionally left uncommitted — bisect
// always reads the eval case from the live working tree, never from git
// history, so it doesn't need to exist at any historical commit.
func setupBisectRepo(t *testing.T) (dir string, shas []string) {
	t.Helper()
	dir = t.TempDir()
	runGitB(t, dir, "init", "-q")
	runGitB(t, dir, "config", "user.email", "test@example.com")
	runGitB(t, dir, "config", "user.name", "Test")

	writeSkill := func(desc string) {
		content := fmt.Sprintf("---\nname: haiku-writer\ndescription: %s\n---\nBody.\n", desc)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) string {
		runGitB(t, dir, "add", ".")
		runGitB(t, dir, "commit", "-q", "-m", msg)
		sha, err := gitutil.RevParseHEAD(dir)
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}

	writeSkill("Writes haikus on request.")
	shas = append(shas, commit("c0: initial"))
	writeSkill("Writes haikus on request, formally.")
	shas = append(shas, commit("c1: add tone"))
	writeSkill("Writes haikus on request, formally, BROKEN.")
	shas = append(shas, commit("c2: culprit"))
	writeSkill("Writes haikus on request, formally, BROKEN, polished.")
	shas = append(shas, commit("c3: later change"))

	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: haiku-case\nprompt: write a haiku\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "case.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir, shas
}

// setupBisectRepoInSubdirectory is the same 4-commit history as
// setupBisectRepo, but the skill lives at skill/SKILL.md — a subdirectory of
// the repo — instead of the repo root. This exercises the RepoRoot +
// filepath.Rel resolution path in bisect.go that setupBisectRepo's
// relPath == "." layout takes a shortcut around.
func setupBisectRepoInSubdirectory(t *testing.T) (dir string, shas []string) {
	t.Helper()
	dir = t.TempDir()
	runGitB(t, dir, "init", "-q")
	runGitB(t, dir, "config", "user.email", "test@example.com")
	runGitB(t, dir, "config", "user.name", "Test")

	skillDir := filepath.Join(dir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeSkill := func(desc string) {
		content := fmt.Sprintf("---\nname: haiku-writer\ndescription: %s\n---\nBody.\n", desc)
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) string {
		runGitB(t, dir, "add", ".")
		runGitB(t, dir, "commit", "-q", "-m", msg)
		sha, err := gitutil.RevParseHEAD(dir)
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}

	writeSkill("Writes haikus on request.")
	shas = append(shas, commit("c0: initial"))
	writeSkill("Writes haikus on request, formally.")
	shas = append(shas, commit("c1: add tone"))
	writeSkill("Writes haikus on request, formally, BROKEN.")
	shas = append(shas, commit("c2: culprit"))
	writeSkill("Writes haikus on request, formally, BROKEN, polished.")
	shas = append(shas, commit("c3: later change"))

	evalsDir := filepath.Join(skillDir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: haiku-case\nprompt: write a haiku\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "case.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir, shas
}

// bisectStubServer fails the case whenever the request (which includes the
// skill's description, per runner.RunCase's system prompt) contains
// "BROKEN" — letting the test drive a deterministic pass/fail sequence
// across the real historical SKILL.md content checked out at each commit.
func bisectStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := "SKILLCI_TRIGGERED: true"
		if strings.Contains(string(body), "BROKEN") {
			text = "SKILLCI_TRIGGERED: false"
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestBisectCmdFindsCulpritWithManualGoodBad(t *testing.T) {
	dir, shas := setupBisectRepo(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"haiku-case", "--path", dir, "--good", shas[0], "--bad", shas[3]})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}
	if !strings.Contains(out.String(), "culprit: "+shas[2]) {
		t.Errorf("output = %q, want it to name culprit %s", out.String(), shas[2])
	}
}

func TestBisectCmdAutoResolvesFromHistory(t *testing.T) {
	dir, shas := setupBisectRepo(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	h := history.History{}
	h.Append(history.Run{CommitSHA: shas[0], Cases: []history.CaseResult{{Name: "haiku-case", Model: "claude-sonnet-5", Passed: true}}})
	h.Append(history.Run{CommitSHA: shas[3], Cases: []history.CaseResult{{Name: "haiku-case", Model: "claude-sonnet-5", Passed: false}}})
	if err := h.Save(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"haiku-case", "--path", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}
	if !strings.Contains(out.String(), "culprit: "+shas[2]) {
		t.Errorf("output = %q, want it to name culprit %s", out.String(), shas[2])
	}
}

// TestBisectCmdFindsCulpritWhenSkillLivesInSubdirectory guards against the
// symlink-resolution bug: --path points at a subdirectory of the repo, so
// bisect.go must resolve absPath through filepath.EvalSymlinks before
// computing relPath via gitutil.RepoRoot, or every worktree checkout below
// would silently read the live tree instead of the historical commit.
func TestBisectCmdFindsCulpritWhenSkillLivesInSubdirectory(t *testing.T) {
	dir, shas := setupBisectRepoInSubdirectory(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"haiku-case", "--path", filepath.Join(dir, "skill"), "--good", shas[0], "--bad", shas[3]})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}
	if !strings.Contains(out.String(), "culprit: "+shas[2]) {
		t.Errorf("output = %q, want it to name culprit %s", out.String(), shas[2])
	}
}

func TestBisectCmdErrorsWhenBadDoesNotReproduce(t *testing.T) {
	dir, shas := setupBisectRepo(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	// shas[1] has no BROKEN marker yet, so it passes — invalid as --bad.
	cmd.SetArgs([]string{"haiku-case", "--path", dir, "--good", shas[0], "--bad", shas[1]})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want an error (bad doesn't reproduce as failing)")
	}
}

func TestBisectCmdErrorsWhenGoodDoesNotVerify(t *testing.T) {
	dir, shas := setupBisectRepo(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	// shas[2] already has the BROKEN marker, so it fails — invalid as --good.
	cmd.SetArgs([]string{"haiku-case", "--path", dir, "--good", shas[2], "--bad", shas[3]})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want an error (good doesn't reproduce as passing)")
	}
}

func TestBisectCmdErrorsWhenNoCommitsInRange(t *testing.T) {
	dir, shas := setupBisectRepo(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	// good == bad: the range is empty, nothing to search.
	cmd.SetArgs([]string{"haiku-case", "--path", dir, "--good", shas[0], "--bad", shas[0]})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want an error (no commits touched the skill directory in range)")
	}
}
