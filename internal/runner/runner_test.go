// internal/runner/runner_test.go
package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/snapshot"
)

func newSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: pr-review\ndescription: Reviews pull requests for SOLID violations.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubServer(t *testing.T, replyText string, inputTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": inputTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func truePtr() *bool    { v := true; return &v }
func intPtr(v int) *int { return &v }

func TestRunCasePassing(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nThis review found a SOLID violation. Overall verdict: REQUEST_CHANGES.", 500)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:           "triggers-on-pr-review-request",
		Prompt:         "Can you review this PR for SOLID violations?",
		SkillUnderTest: "pr-review",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			Contains:        []string{"SOLID", "verdict"},
			NotContains:     []string{"I cannot"},
			MaxTokensLoaded: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Triggered {
		t.Error("Triggered = false, want true")
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsOnMissingContains(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nLooks fine to me.", 500)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Contains:  []string{"SOLID"},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (missing required substring)")
	}
}

func TestRunCaseFailsOnUnexpectedTrigger(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: false", 200)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	falsePtr := func() *bool { v := false; return &v }
	c := evalspec.Case{
		Name:   "should-not-trigger",
		Prompt: "what's the weather",
		Assert: evalspec.Assertions{Triggered: falsePtr()},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true (correctly did not trigger); Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsOnTokenBudget(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nSOLID verdict here.", 5000)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "budget-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			MaxTokensLoaded: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (5000 tokens exceeds 3000 budget)")
	}
}

func TestRunCaseSnapshotFirstRunCapturesGolden(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nA haiku about autumn leaves.", 100)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)
	c := evalspec.Case{
		Name:   "snap-case",
		Prompt: "write a haiku",
		Assert: evalspec.Assertions{Snapshot: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.SnapshotDiff != nil {
		t.Errorf("SnapshotDiff = %+v, want nil on first run (nothing to compare against)", result.SnapshotDiff)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true — first-run capture must not fail the case")
	}

	golden, ok, err := snapshot.Load(dir, "snap-case", "claude-sonnet-5")
	if err != nil || !ok {
		t.Fatalf("golden not saved after first run: ok=%v err=%v", ok, err)
	}
	if golden == "" {
		t.Error("saved golden text is empty")
	}
}

func TestRunCaseSnapshotUnchangedPasses(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nA haiku about autumn leaves.", 100)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)
	c := evalspec.Case{
		Name:   "snap-case",
		Prompt: "write a haiku",
		Assert: evalspec.Assertions{Snapshot: truePtr()},
	}

	if _, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c); err != nil {
		t.Fatalf("first RunCase() error = %v", err)
	}
	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("second RunCase() error = %v", err)
	}
	if result.SnapshotDiff != nil {
		t.Errorf("SnapshotDiff = %+v, want nil when response is identical to golden", result.SnapshotDiff)
	}
	if !result.Passed {
		t.Error("Passed = false, want true for an unchanged snapshot")
	}
}

func TestRunCaseSnapshotChangedNonStrictStillPasses(t *testing.T) {
	dir := newSkillDir(t)
	if err := snapshot.Save(dir, "snap-case", "claude-sonnet-5", "Old leaves drift and fall."); err != nil {
		t.Fatalf("seeding golden: %v", err)
	}

	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nOld leaves drift and settle.", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	c := evalspec.Case{
		Name:   "snap-case",
		Prompt: "write a haiku",
		Assert: evalspec.Assertions{Snapshot: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.SnapshotDiff == nil || !result.SnapshotDiff.Changed {
		t.Fatal("SnapshotDiff = nil or unchanged, want a detected change")
	}
	if !result.Passed {
		t.Error("Passed = false, want true — non-strict snapshot changes must not fail the case")
	}

	pending, ok, err := snapshot.LoadPending(dir, "snap-case", "claude-sonnet-5")
	if err != nil || !ok {
		t.Fatalf("pending snapshot not saved: ok=%v err=%v", ok, err)
	}
	if pending == "" {
		t.Error("saved pending text is empty")
	}
}

func TestRunCaseSnapshotChangedStrictFails(t *testing.T) {
	dir := newSkillDir(t)
	if err := snapshot.Save(dir, "snap-case", "claude-sonnet-5", "Old leaves drift and fall."); err != nil {
		t.Fatalf("seeding golden: %v", err)
	}

	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nOld leaves drift and settle.", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	c := evalspec.Case{
		Name:   "snap-case",
		Prompt: "write a haiku",
		Assert: evalspec.Assertions{Snapshot: truePtr(), SnapshotStrict: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — snapshot_strict must fail the case on a detected diff")
	}
	if len(result.Failures) == 0 {
		t.Error("Failures is empty, want a snapshot-changed failure message")
	}
}

func TestRunCaseSnapshotSkippedWhenOtherAssertionFails(t *testing.T) {
	// Regression test: a case asserting Triggered=true and Snapshot=true
	// whose response unexpectedly doesn't trigger must NOT save an empty
	// golden baseline. The triggered-mismatch failure should win, and the
	// snapshot block must not run at all.
	srv := stubServer(t, "SKILLCI_TRIGGERED: false", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)

	c := evalspec.Case{
		Name:   "snap-case",
		Prompt: "write a haiku",
		Assert: evalspec.Assertions{Triggered: truePtr(), Snapshot: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (case did not trigger as asserted)")
	}
	foundTriggeredFailure := false
	for _, f := range result.Failures {
		if strings.Contains(f, "triggered") {
			foundTriggeredFailure = true
		}
		if strings.Contains(f, "snapshot") {
			t.Errorf("Failures contains a snapshot failure %q, want only the triggered-mismatch failure", f)
		}
	}
	if !foundTriggeredFailure {
		t.Errorf("Failures = %v, want a triggered-mismatch message", result.Failures)
	}

	if _, ok, err := snapshot.Load(dir, "snap-case", "claude-sonnet-5"); err != nil {
		t.Fatalf("snapshot.Load() error = %v", err)
	} else if ok {
		t.Error("a golden file was saved from a failed run — empty-golden poisoning bug not fixed")
	}
}

func TestRunCaseSnapshotNotEnabledNoDiffField(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nSome response.", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)

	c := evalspec.Case{
		Name:   "plain-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{Triggered: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.SnapshotDiff != nil {
		t.Errorf("SnapshotDiff = %+v, want nil when Snapshot assertion is not set", result.SnapshotDiff)
	}
	if _, ok, _ := snapshot.Load(dir, "plain-case", "claude-sonnet-5"); ok {
		t.Error("a golden file was written even though Snapshot was not requested")
	}
}

func TestRunCaseFuzzFlippedNonStrictStillPasses(t *testing.T) {
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
			// The negation mutation that inserts "don't" before the verb
			// flips the outcome to not-triggered.
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

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)
	c := evalspec.Case{
		Name:   "fuzz-case",
		Prompt: "Can you write me a haiku?",
		Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true — non-strict fuzz flips must not fail the case; Failures = %v", result.Failures)
	}
	if len(result.FuzzFindings) == 0 {
		t.Fatal("FuzzFindings is empty, want mutations recorded")
	}
	sawFlip := false
	for _, f := range result.FuzzFindings {
		if f.Flipped {
			sawFlip = true
		}
	}
	if !sawFlip {
		t.Errorf("FuzzFindings = %+v, want at least one Flipped=true finding (the don't-insertion negation mutation)", result.FuzzFindings)
	}
}

func TestRunCaseFuzzFlippedStrictFails(t *testing.T) {
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

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)
	c := evalspec.Case{
		Name:   "fuzz-case",
		Prompt: "Can you write me a haiku?",
		Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr(), FuzzStrict: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — fuzz_strict must fail the case on a flipped mutation")
	}
}

func TestRunCaseFuzzSkippedWhenOtherAssertionFails(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: false", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)

	c := evalspec.Case{
		Name:   "fuzz-case",
		Prompt: "Can you write me a haiku?",
		Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (did not trigger as asserted)")
	}
	if len(result.FuzzFindings) != 0 {
		t.Errorf("FuzzFindings = %+v, want none — a case that already failed its own assertions must not be fuzzed", result.FuzzFindings)
	}
}

func TestRunCaseFuzzNotEnabledNoFindings(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nA haiku.", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)

	c := evalspec.Case{
		Name:   "plain-case",
		Prompt: "Can you write me a haiku?",
		Assert: evalspec.Assertions{Triggered: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if len(result.FuzzFindings) != 0 {
		t.Errorf("FuzzFindings = %+v, want none when Fuzz assertion is not set", result.FuzzFindings)
	}
}

func TestRunCaseFuzzSkippedWithoutTriggeredAssertion(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nA haiku.", 100)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)

	c := evalspec.Case{
		Name:   "no-triggered-case",
		Prompt: "Can you write me a haiku?",
		Assert: evalspec.Assertions{Fuzz: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if len(result.FuzzFindings) != 0 {
		t.Errorf("FuzzFindings = %+v, want none — fuzz has nothing to compare against without a triggered assertion", result.FuzzFindings)
	}
}
