package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kabirnarang39/skillci/internal/gitutil"
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/kabirnarang39/skillci/internal/snapshot"
	"github.com/kabirnarang39/skillci/internal/upload"
)

func setupSkillWithCase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skillContent := "---\nname: pr-review\ndescription: Reviews PRs.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: c1\nprompt: review this\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRegressCommandCommitSHAFallsBackToLocalGitHEAD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t)
	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial")
	wantSHA, err := gitutil.RevParseHEAD(dir)
	if err != nil {
		t.Fatalf("RevParseHEAD() error = %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)
	// GitHub Actions always sets GITHUB_SHA for the runner's own commit, so
	// explicitly clear it rather than relying on it being absent from the
	// ambient environment — this test is specifically about the fallback
	// path, which only triggers when GITHUB_SHA is genuinely empty.
	t.Setenv("GITHUB_SHA", "")

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	h, err := history.Load(filepath.Join(dir, ".skillci", "history.json"))
	if err != nil {
		t.Fatalf("history.Load() error = %v", err)
	}
	lastRun, ok := h.LastRun()
	if !ok {
		t.Fatal("history has no runs")
	}
	if lastRun.CommitSHA != wantSHA {
		t.Errorf("CommitSHA = %q, want %q (local git HEAD)", lastRun.CommitSHA, wantSHA)
	}
}

func TestRegressCommandCommitSHAEmptyWhenNoGitAndNoEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t) // plain temp dir, not a git repo
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)
	// GitHub Actions always sets GITHUB_SHA for the runner's own commit, so
	// explicitly clear it rather than relying on it being absent from the
	// ambient environment.
	t.Setenv("GITHUB_SHA", "")

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	h, err := history.Load(filepath.Join(dir, ".skillci", "history.json"))
	if err != nil {
		t.Fatalf("history.Load() error = %v", err)
	}
	lastRun, ok := h.LastRun()
	if !ok {
		t.Fatal("history has no runs")
	}
	if lastRun.CommitSHA != "" {
		t.Errorf("CommitSHA = %q, want empty (not a git repo, no GITHUB_SHA)", lastRun.CommitSHA)
	}
}

func TestRegressCommandSuggestsBisectOnNewRegression(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text := "SKILLCI_TRIGGERED: true"
		if atomic.LoadInt32(&calls) > 0 {
			text = "SKILLCI_TRIGGERED: false"
		}
		atomic.AddInt32(&calls, 1)
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t) // case c1 asserts triggered: true
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	// First run: passes (triggered: true), establishes history.
	cmd1 := newRegressCmd()
	cmd1.SetOut(&bytes.Buffer{})
	cmd1.SetArgs([]string{dir})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	// Second run: now fails (triggered: false) — a new regression.
	cmd2 := newRegressCmd()
	var out bytes.Buffer
	cmd2.SetOut(&out)
	cmd2.SetArgs([]string{dir})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("second Execute() error = nil, want an error (fail_on=regression default)")
	}

	if !strings.Contains(out.String(), "skillci bisect c1") {
		t.Errorf("output = %q, want a bisect suggestion mentioning case c1", out.String())
	}
}

func TestRegressCommandNoPriorHistoryDoesNotFailCI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil (first run, nothing to regress from); output = %s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Errorf("history.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".skillci", "badge.svg")); err != nil {
		t.Errorf("badge.svg not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evals", "_generated")); err != nil {
		t.Errorf("evals/_generated not created for the uncovered failing case: %v", err)
	}
}

func TestRegressCommandUploadFailureDoesNotFailCI(t *testing.T) {
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer modelSrv.Close()

	dashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dashSrv.Close()

	dir := setupSkillWithCase(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", modelSrv.URL)
	t.Setenv("SKILLCI_DASHBOARD_URL", dashSrv.URL)
	t.Setenv("SKILLCI_INGEST_TOKEN", "secret-token")

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--upload", dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil — a dashboard upload failure must not fail CI (design §8)", err)
	}
}

func TestRegressCommandRecordsTimestampAndCommitSHAInLocalHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)
	t.Setenv("GITHUB_SHA", "abc123")

	before := time.Now()

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	h, err := history.Load(filepath.Join(dir, ".skillci", "history.json"))
	if err != nil {
		t.Fatalf("history.Load() error = %v", err)
	}
	lastRun, ok := h.LastRun()
	if !ok {
		t.Fatal("history has no runs")
	}
	if lastRun.Timestamp.IsZero() || lastRun.Timestamp.Before(before) {
		t.Errorf("Timestamp = %v, want non-zero and >= test start (%v)", lastRun.Timestamp, before)
	}
	if lastRun.CommitSHA != "abc123" {
		t.Errorf("CommitSHA = %q, want %q (from GITHUB_SHA)", lastRun.CommitSHA, "abc123")
	}
}

// TestRegressCommandUploadAggregatesPerModel proves two things about the
// upload path: (a) the uploaded skill name resolves correctly even when the
// command is invoked with dir="." from inside the skill directory (rather
// than literally uploading "."), and (b) multiple eval cases for the same
// model are aggregated into a single upload.Send call per model — not one
// per case — with Passed true only if every case for that model passed.
func TestRegressCommandUploadAggregatesPerModel(t *testing.T) {
	// The mock Anthropic server always reports triggered:true. Combined with
	// one case asserting triggered:true and another asserting triggered:false,
	// this produces one passing and one failing case for the same (single,
	// default) model — a mixed result that must collapse to Passed=false.
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer modelSrv.Close()

	var mu sync.Mutex
	var received []upload.Result
	dashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var res upload.Result
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		mu.Lock()
		received = append(received, res)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer dashSrv.Close()

	dir := t.TempDir()
	skillContent := "---\nname: pr-review\ndescription: Reviews PRs.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.yaml"), []byte("name: c1\nprompt: p1\nassert:\n  triggered: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evalsDir, "c2.yaml"), []byte("name: c2\nprompt: p2\nassert:\n  triggered: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origWD) })

	// Compute the expected skill name the same way the production code
	// resolves it (filepath.Abs("."), which internally calls Getwd), so the
	// assertion is independent of any symlink resolution quirks of t.TempDir().
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	wantSkill := filepath.Base(cwd)

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", modelSrv.URL)
	t.Setenv("SKILLCI_DASHBOARD_URL", dashSrv.URL)
	t.Setenv("SKILLCI_INGEST_TOKEN", "secret-token")

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--upload", "."})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("dashboard received %d requests, want 1 (one upload per model, not per case): %+v", len(received), received)
	}
	got := received[0]
	if got.Skill != wantSkill {
		t.Errorf("Skill = %q, want %q (dir=\".\" must resolve via filepath.Abs, not upload literal \".\")", got.Skill, wantSkill)
	}
	if got.Model != "claude-sonnet-5" {
		t.Errorf("Model = %q, want claude-sonnet-5", got.Model)
	}
	if got.Passed {
		t.Errorf("Passed = true, want false: one of two cases for this model failed, so the aggregate must be false")
	}
}

func TestAcceptCommandPromotesGeneratedCase(t *testing.T) {
	dir := setupSkillWithCase(t)
	genDir := filepath.Join(dir, "evals", "_generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	genPath := filepath.Join(genDir, "new-case.yaml")
	if err := os.WriteFile(genPath, []byte("name: new-case\nprompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newAcceptCmd()
	cmd.SetArgs([]string{"new-case", "--path", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(genPath); !os.IsNotExist(err) {
		t.Error("generated file still exists in _generated after accept")
	}
	if _, err := os.Stat(filepath.Join(dir, "evals", "new-case.yaml")); err != nil {
		t.Errorf("promoted file not found in evals/: %v", err)
	}
}

func TestRegressCommandPrintsSnapshotDiffWhenChanged(t *testing.T) {
	dir := setupSkillWithCase(t)
	evalsDir := filepath.Join(dir, "evals")
	caseContent := "name: c1\nprompt: review this\nassert:\n  triggered: true\n  snapshot: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.claude-sonnet-5.golden.txt"), []byte("first version"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true\nsecond version"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil; output = %s", err, out.String())
	}
	if !strings.Contains(out.String(), "SNAPSHOT CHANGED") {
		t.Errorf("output = %q, want it to mention SNAPSHOT CHANGED", out.String())
	}
}

func TestRegressCommandPrintsFuzzFlipsWhenMutationFlips(t *testing.T) {
	dir := setupSkillWithCase(t)
	evalsDir := filepath.Join(dir, "evals")
	caseContent := "name: c1\nprompt: Can you write me a haiku?\nassert:\n  triggered: true\n  fuzz: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		text := "SKILLCI_TRIGGERED: true"
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "don't") {
			// The negation mutation that inserts "don't" before the verb flips the outcome
			text = "SKILLCI_TRIGGERED: false"
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil; output = %s", err, out.String())
	}
	if !strings.Contains(out.String(), "[FUZZ]") {
		t.Errorf("output = %q, want it to mention [FUZZ]", out.String())
	}
}

func TestAcceptCommandPromotesSnapshotWithModelFlag(t *testing.T) {
	dir := t.TempDir()
	if err := snapshot.Save(dir, "my-case", "claude-sonnet-5", "old text"); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.SavePending(dir, "my-case", "claude-sonnet-5", "new text"); err != nil {
		t.Fatal(err)
	}

	cmd := newAcceptCmd()
	cmd.SetArgs([]string{"my-case", "--model", "claude-sonnet-5", "--path", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	golden, ok, err := snapshot.Load(dir, "my-case", "claude-sonnet-5")
	if err != nil || !ok {
		t.Fatalf("golden not found after accept: ok=%v err=%v", ok, err)
	}
	if golden != "new text" {
		t.Errorf("golden = %q, want %q", golden, "new text")
	}
	if _, ok, _ := snapshot.LoadPending(dir, "my-case", "claude-sonnet-5"); ok {
		t.Error("pending snapshot still exists after accept")
	}
}

func TestAcceptCommandModelFlagErrorsWithNoPendingSnapshot(t *testing.T) {
	dir := t.TempDir()
	cmd := newAcceptCmd()
	cmd.SetArgs([]string{"no-such-case", "--model", "claude-sonnet-5", "--path", dir})

	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want error when no pending snapshot exists")
	}
}

func TestRegressCommandPrintsLatencyWarningWhenExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true\nhi"}},
			"usage":   map[string]int{"input_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t)
	caseContent := "name: c1\nprompt: review this\nassert:\n  triggered: true\n  max_latency_ms: 1\n"
	if err := os.WriteFile(filepath.Join(dir, "evals", "c1.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	if !strings.Contains(out.String(), "[LATENCY]") {
		t.Errorf("output = %q, want a [LATENCY] line", out.String())
	}
}
