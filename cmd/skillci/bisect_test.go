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
	"sync/atomic"
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

// bisectStubServerCounting behaves like bisectStubServer but also counts
// every request it receives, via the returned *int64 (read with
// atomic.LoadInt64), so a test can prove a second bisect invocation makes
// fewer API calls than the first because it reused cached results.
func bisectStubServerCounting(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
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
	return srv, &calls
}

// TestBisectCmdReusesCachedResultsAcrossInvocations proves the persistent
// .skillci/bisect-cache.json actually gets consulted on a second, separate
// `skillci bisect` invocation on the same case — not just within one run's
// in-memory `verified` map, which never survives past a single Execute().
func TestBisectCmdReusesCachedResultsAcrossInvocations(t *testing.T) {
	dir, shas := setupBisectRepo(t)
	srv, calls := bisectStubServerCounting(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	args := []string{"haiku-case", "--path", dir, "--good", shas[0], "--bad", shas[3]}

	cmd1 := newBisectCmd()
	var out1 bytes.Buffer
	cmd1.SetOut(&out1)
	cmd1.SetArgs(args)
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first Execute() error = %v; output = %s", err, out1.String())
	}
	if strings.Contains(out1.String(), "(cached)") {
		t.Errorf("first run output = %q, want no cache hits on a fresh cache", out1.String())
	}
	firstRunCalls := atomic.LoadInt64(calls)
	if firstRunCalls == 0 {
		t.Fatal("first run made zero API calls — test setup is broken")
	}

	if _, err := os.Stat(filepath.Join(dir, ".skillci", "bisect-cache.json")); err != nil {
		t.Fatalf(".skillci/bisect-cache.json was not created by the first run: %v", err)
	}

	cmd2 := newBisectCmd()
	var out2 bytes.Buffer
	cmd2.SetOut(&out2)
	cmd2.SetArgs(args)
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second Execute() error = %v; output = %s", err, out2.String())
	}
	if !strings.Contains(out2.String(), "culprit: "+shas[2]) {
		t.Errorf("second run output = %q, want it to still name culprit %s", out2.String(), shas[2])
	}
	if cacheHits := strings.Count(out2.String(), "(cached)"); cacheHits < 2 {
		t.Errorf("second run had %d cache hits, want at least 2 (good and bad endpoints reused from the persisted cache)", cacheHits)
	}
	secondRunNewCalls := atomic.LoadInt64(calls) - firstRunCalls
	if secondRunNewCalls >= firstRunCalls {
		t.Errorf("second run made %d new API calls (first run made %d) — want fewer, since good/bad and all candidates should already be cached", secondRunNewCalls, firstRunCalls)
	}
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

// TestBisectCmdReportsCulpritWhenDiffFails guards the fallback added for a
// culprit that has no parent commit (gitutil.DiffFiles(culprit+"^", culprit)
// then fails): the culprit's SHA/author/date/message must still be printed,
// with a one-line fallback note in place of the diff, and the command must
// still succeed.
//
// The only way to reach this from the real bisect flow is for LogPaths'
// (good, bad] range to include a rootless commit — which happens if --good
// isn't actually an ancestor of --bad (e.g. a user-supplied SHA from an
// unrelated branch/history). LogPaths shells out to `git log good..bad`,
// pure SHA reachability with no ancestry requirement, so a disjoint --good
// still yields --bad's own (single-commit, rootless) history as the
// candidate range.
func TestBisectCmdReportsCulpritWhenDiffFails(t *testing.T) {
	dir := t.TempDir()
	runGitB(t, dir, "init", "-q")
	runGitB(t, dir, "config", "user.email", "test@example.com")
	runGitB(t, dir, "config", "user.name", "Test")

	writeSkill := func(desc string) {
		content := fmt.Sprintf("---\nname: haiku-writer\ndescription: %s\n---\nBody.\n", desc)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: haiku-case\nprompt: write a haiku\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "case.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// bad: the repo's one and only commit — a root commit with no parent,
	// already broken.
	writeSkill("Writes haikus on request, BROKEN.")
	runGitB(t, dir, "add", ".")
	runGitB(t, dir, "commit", "-q", "-m", "root: broken from the start")
	badSHA, err := gitutil.RevParseHEAD(dir)
	if err != nil {
		t.Fatal(err)
	}

	// good: an unrelated orphan commit (disjoint history) that passes.
	runGitB(t, dir, "checkout", "-q", "--orphan", "good-branch")
	writeSkill("Writes haikus on request.")
	runGitB(t, dir, "add", ".")
	runGitB(t, dir, "commit", "-q", "-m", "unrelated good baseline")
	goodSHA, err := gitutil.RevParseHEAD(dir)
	if err != nil {
		t.Fatal(err)
	}

	srv := bisectStubServer(t)
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"haiku-case", "--path", dir, "--good", goodSHA, "--bad", badSHA})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}
	if !strings.Contains(out.String(), "culprit: "+badSHA) {
		t.Errorf("output = %q, want it to name culprit %s even though its diff can't be computed", out.String(), badSHA)
	}
	if !strings.Contains(out.String(), "could not show a diff") {
		t.Errorf("output = %q, want a fallback note explaining the diff couldn't be shown", out.String())
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

// setupBisectRepoWithMerge builds a history where the real regression is
// introduced on a feature branch, an UNRELATED commit lands on main in the
// meantime, and the two are merged — producing a genuinely non-monotonic
// pass/fail sequence in `git log --reverse` order (fail, pass, fail) that
// Search's binary-search assumption cannot represent correctly, but which
// SearchLinear (bisect.go's merge-detection fallback) can.
func setupBisectRepoWithMerge(t *testing.T) (dir, good, bad, culprit string) {
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
	good = commit("c0: initial")

	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		t.Fatal(err)
	}
	baseBranchName := strings.TrimSpace(string(out))

	runGitB(t, dir, "checkout", "-q", "-b", "feature")
	writeSkill("Writes haikus on request, formally, BROKEN.")
	culprit = commit("c1: feature — introduces the real regression")

	runGitB(t, dir, "checkout", "-q", baseBranchName)
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated change"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit("c2: unrelated change on main, SKILL.md untouched")

	runGitB(t, dir, "merge", "-q", "--no-ff", "feature", "-m", "c3: merge feature into main")
	bad, err = gitutil.RevParseHEAD(dir)
	if err != nil {
		t.Fatal(err)
	}

	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: haiku-case\nprompt: write a haiku\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "case.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir, good, bad, culprit
}

func TestBisectCmdHandlesMergeCommitHistory(t *testing.T) {
	dir, good, bad, culprit := setupBisectRepoWithMerge(t)
	srv := bisectStubServer(t)
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newBisectCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"haiku-case", "--path", dir, "--good", good, "--bad", bad})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	output := out.String()
	if !strings.Contains(output, "merge commit(s) detected") {
		t.Errorf("output = %q, want it to report detecting a merge commit and using the linear-scan fallback", output)
	}
	if !strings.Contains(output, "culprit: "+culprit) {
		t.Errorf("output = %q, want it to name the real culprit %s (the feature-branch commit), not be confused by the merge or the unrelated main-branch commit", output, culprit)
	}
	if !strings.Contains(output, "non-linear") {
		t.Errorf("output = %q, want a warning that the history is non-linear (a second pass-to-fail transition exists at the merge commit)", output)
	}
}
