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
	"time"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/config"
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

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	if _, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, ""); err != nil {
		t.Fatalf("first RunCase() error = %v", err)
	}
	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
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

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if len(result.FuzzFindings) != 0 {
		t.Errorf("FuzzFindings = %+v, want none — fuzz has nothing to compare against without a triggered assertion", result.FuzzFindings)
	}
}

// TestRunCaseSnapshotAndFuzzBothEnabledProduceBothArtifacts is a regression
// test for the final whole-branch review's Minor gap: RunCase runs the
// snapshot block (touches only the primary response's content) before the
// fuzz block (touches only each mutation's parsed trigger outcome), so
// enabling both on the same case must produce a saved golden snapshot AND
// fuzz findings, with neither block corrupting the other's state. If a
// future edit reorders the two blocks, this test should catch it.
func TestRunCaseSnapshotAndFuzzBothEnabledProduceBothArtifacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		text := "SKILLCI_TRIGGERED: true\nA haiku about autumn leaves."
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "don't") {
			text = "SKILLCI_TRIGGERED: false"
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 100},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)
	c := evalspec.Case{
		Name:   "both-case",
		Prompt: "Can you write me a haiku?",
		Assert: evalspec.Assertions{Triggered: truePtr(), Snapshot: truePtr(), Fuzz: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true (non-strict fuzz); Failures = %v", result.Failures)
	}

	if result.SnapshotDiff != nil {
		t.Errorf("SnapshotDiff = %+v, want nil on first run (nothing to compare against)", result.SnapshotDiff)
	}
	golden, ok, err := snapshot.Load(dir, "both-case", "claude-sonnet-5")
	if err != nil || !ok {
		t.Fatalf("golden not saved after first run: ok=%v err=%v", ok, err)
	}
	if golden == "" {
		t.Error("saved golden text is empty")
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

// stubServerWithUsage is like the existing stubServer but also sets
// output_tokens and can simulate latency via a deliberate delay — needed
// for the new output-tokens/latency/cost tests without changing the
// existing stubServer helper (which every pre-existing test still uses).
func stubServerWithUsage(t *testing.T, replyText string, inputTokens, outputTokens int, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": inputTokens, "output_tokens": outputTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func int64Ptr(v int64) *int64       { return &v }
func float64Ptr(v float64) *float64 { return &v }

func TestRunCaseFailsOnOutputTokenBudget(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nSOLID verdict here.", 100, 5000, 0)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "output-budget-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			MaxOutputTokens: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (5000 output tokens exceeds 3000 budget)")
	}
	if result.OutputTokens != 5000 {
		t.Errorf("OutputTokens = %d, want 5000", result.OutputTokens)
	}
}

func TestRunCasePassesUnderOutputTokenBudget(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nSOLID verdict here.", 100, 200, 0)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "output-budget-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			MaxOutputTokens: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
}

func TestRunCaseOutputTokensAlwaysPopulated(t *testing.T) {
	// ponytail: a zero delay lets a fast loopback round-trip finish in
	// under 1ms, which Latency.Milliseconds() truncates to 0 — flaky, not
	// an implementation bug. A tiny deterministic delay keeps the assertion
	// meaningful without weakening it.
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nhi", 100, 42, 2*time.Millisecond)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "no-assertion-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{Triggered: truePtr()},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want 42 (always populated, no assertion needed)", result.OutputTokens)
	}
	if result.LatencyMs <= 0 {
		t.Errorf("LatencyMs = %d, want > 0 (always populated)", result.LatencyMs)
	}
}

func TestRunCaseLatencyNonStrictInformationalOnly(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nhi", 100, 10, 60*time.Millisecond)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "latency-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:    truePtr(),
			MaxLatencyMs: int64Ptr(10),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true — non-strict latency violation must not fail the case; Failures = %v", result.Failures)
	}
	if !result.LatencyExceeded {
		t.Error("LatencyExceeded = false, want true (measured latency exceeded the 10ms cap)")
	}
}

func TestRunCaseLatencyStrictFails(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nhi", 100, 10, 60*time.Millisecond)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "latency-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:     truePtr(),
			MaxLatencyMs:  int64Ptr(10),
			LatencyStrict: truePtr(),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — latency_strict must fail the case on an exceeded cap")
	}
}

func TestRunCaseLatencyNotExceededWhenUnderCap(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nhi", 100)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "latency-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:    truePtr(),
			MaxLatencyMs: int64Ptr(10000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.LatencyExceeded {
		t.Error("LatencyExceeded = true, want false (measured latency well under the 10000ms cap)")
	}
}

func TestRunCaseFailsOnCostBudget(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nhi", 1_000_000, 1_000_000, 0)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "cost-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:  truePtr(),
			MaxCostUSD: float64Ptr(1.0),
		},
	}
	pricing := map[string]config.ModelPricing{
		"claude-sonnet-5": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	}

	// 1M input tokens * $3/M + 1M output tokens * $15/M = $18, exceeds $1 cap.
	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, pricing, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (computed cost of $18 exceeds $1 cap)")
	}
}

func TestRunCasePassesUnderCostBudget(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nhi", 100, 100, 0)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "cost-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:  truePtr(),
			MaxCostUSD: float64Ptr(1.0),
		},
	}
	pricing := map[string]config.ModelPricing{
		"claude-sonnet-5": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, pricing, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsHardOnMissingPricingForCostAssertion(t *testing.T) {
	srv := stubServerWithUsage(t, "SKILLCI_TRIGGERED: true\nhi", 100, 100, 0)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "cost-case-no-pricing",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:  truePtr(),
			MaxCostUSD: float64Ptr(1.0),
		},
	}

	// pricing is nil — no entry for claude-sonnet-5 at all.
	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — max_cost_usd with no pricing entry must hard-fail")
	}
	found := false
	for _, f := range result.Failures {
		if strings.Contains(f, "no pricing configured for model") {
			found = true
		}
	}
	if !found {
		t.Errorf("Failures = %v, want a message naming the missing pricing configuration", result.Failures)
	}
}

// sequencedStubServer returns a stub server that replies with texts[i] on
// the (i+1)th request it receives (1-indexed), and repeats the last entry
// for any request beyond len(texts) — plus a pointer to the live call
// count so tests can assert exactly how many requests were actually made
// (proving early-stop behavior, not just the final verdict).
func sequencedStubServer(t *testing.T, texts []string) (*httptest.Server, *int) {
	t.Helper()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := callCount
		if idx >= len(texts) {
			idx = len(texts) - 1
		}
		callCount++
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": texts[idx]}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return srv, &callCount
}

func TestRunCaseFlakeRetriesConfirmedPassAfterInitialFailure(t *testing.T) {
	// Attempt 1 fails (false), attempts 2-3 pass (true) — majority passes.
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true",
		"SKILLCI_TRIGGERED: true",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(2)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if result.FlakeVerdict != "confirmed_pass" {
		t.Errorf("FlakeVerdict = %q, want confirmed_pass", result.FlakeVerdict)
	}
	if *callCount != 3 {
		t.Errorf("callCount = %d, want 3 (all attempts needed to reach a majority)", *callCount)
	}
}

func TestRunCaseFlakeRetriesConfirmedPassSkipsSnapshot(t *testing.T) {
	// Regression test for the flake_retries + snapshot interaction: attempt 1
	// fails its trigger check (that's what fires a retry at all), attempts
	// 2-3 pass, and the vote resolves to confirmed_pass. Attempt 1's content
	// is a REJECTED, failing response — it must never be saved as the golden
	// baseline just because result.Failures ends up empty. Snapshotting
	// should be skipped entirely whenever a flake retry fired, not just when
	// other assertions fail (see TestRunCaseSnapshotSkippedWhenOtherAssertionFails
	// for that sibling case).
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true\nA haiku about autumn leaves.",
		"SKILLCI_TRIGGERED: true\nA haiku about autumn leaves.",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	dir := newSkillDir(t)
	c := evalspec.Case{
		Name:   "flake-snap-case",
		Prompt: "write a haiku",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(2), Snapshot: truePtr()},
	}

	result, err := RunCase(context.Background(), client, dir, "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if result.FlakeVerdict != "confirmed_pass" {
		t.Errorf("FlakeVerdict = %q, want confirmed_pass", result.FlakeVerdict)
	}
	if *callCount != 3 {
		t.Errorf("callCount = %d, want 3", *callCount)
	}
	if result.SnapshotDiff != nil {
		t.Errorf("SnapshotDiff = %+v, want nil — snapshotting must be skipped when flake retries fired", result.SnapshotDiff)
	}
	if _, ok, err := snapshot.Load(dir, "flake-snap-case", "claude-sonnet-5"); err != nil {
		t.Fatalf("snapshot.Load() error = %v", err)
	} else if ok {
		t.Error("a golden file was saved despite flake retries firing — attempt 1's rejected content was poisoned into the baseline")
	}
}

func TestRunCaseFlakeRetriesConfirmedFail(t *testing.T) {
	// FlakeRetries: 2 allows at most 3 total attempts. The first 2 attempts
	// both fail, and with only 1 attempt remaining a pass there could not
	// overturn a 2-0 deficit (1 < 2), so the majority is already decided
	// after attempt 2 — the same early-stop math exercised by
	// TestRunCaseFlakeRetriesEarlyStopsOnDecidedMajority. The 3rd reply
	// exists only to prove it is never consumed.
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: false", // must never be reached — decided at attempt 2
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(2)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — all attempts failed")
	}
	if result.FlakeVerdict != "confirmed_fail" {
		t.Errorf("FlakeVerdict = %q, want confirmed_fail", result.FlakeVerdict)
	}
	if *callCount != 2 {
		t.Errorf("callCount = %d, want 2 (majority decided after 2 fails; the 3rd attempt must never be made)", *callCount)
	}
}

func TestRunCaseFlakeRetriesEarlyStopsOnDecidedMajority(t *testing.T) {
	// flake_retries: 4 allows up to 5 attempts, but the first 3 all fail:
	// with fails=3 and only 2 attempts remaining, passes can never catch
	// up (3 > 0+2), so the vote is decided after attempt 3 — attempts 4-5
	// must never be made.
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true", // must never be reached
		"SKILLCI_TRIGGERED: true", // must never be reached
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(4)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false")
	}
	if *callCount != 3 {
		t.Errorf("callCount = %d, want 3 (must stop early once the majority is decided, not spend the full 5-attempt budget)", *callCount)
	}
	if result.FlakeAttemptsTotal != 3 {
		t.Errorf("FlakeAttemptsTotal = %d, want 3", result.FlakeAttemptsTotal)
	}
}

func TestRunCaseFlakeRetriesUnstableTieNonStrictInformationalOnly(t *testing.T) {
	// flake_retries: 1 allows 2 total attempts, 1 pass + 1 fail = tie.
	srv, _ := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(1)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.FlakeVerdict != "unstable" {
		t.Errorf("FlakeVerdict = %q, want unstable", result.FlakeVerdict)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true — a non-strict tie must not fail the case; Failures = %v", result.Failures)
	}
}

func TestRunCaseFlakeRetriesUnstableTieStrictFails(t *testing.T) {
	srv, _ := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(1), FlakeStrict: truePtr()},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.FlakeVerdict != "unstable" {
		t.Errorf("FlakeVerdict = %q, want unstable", result.FlakeVerdict)
	}
	if result.Passed {
		t.Error("Passed = true, want false — flake_strict must fail an unresolved tie")
	}
}

func TestRunCaseFlakeRetriesNotTriggeredWhenFirstAttemptPasses(t *testing.T) {
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: true",
		"SKILLCI_TRIGGERED: false", // must never be reached
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(2)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if result.FlakeVerdict != "" {
		t.Errorf("FlakeVerdict = %q, want empty — no retry should have been attempted", result.FlakeVerdict)
	}
	if *callCount != 1 {
		t.Errorf("callCount = %d, want 1 — a passing first attempt must never trigger a retry", *callCount)
	}
}

func TestRunCaseFlakeStrictAloneWithoutFlakeRetriesIsInert(t *testing.T) {
	// flake_strict: true with no flake_retries set at all — per the
	// design's error-handling section, this must be inert config, not an
	// error, and must not trigger any retry (only FlakeRetries>0 does).
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true", // must never be reached
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeStrict: truePtr()},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — the single attempt failed and no retry should have run to change that")
	}
	if result.FlakeVerdict != "" {
		t.Errorf("FlakeVerdict = %q, want empty — flake_strict alone must not trigger a retry", result.FlakeVerdict)
	}
	if *callCount != 1 {
		t.Errorf("callCount = %d, want 1 — flake_strict without flake_retries must be inert", *callCount)
	}
}

func TestRunCaseFlakeRetriesExplicitZeroBehavesLikeUnset(t *testing.T) {
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true", // must never be reached
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(0)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false")
	}
	if result.FlakeVerdict != "" {
		t.Errorf("FlakeVerdict = %q, want empty — flake_retries: 0 must behave identically to unset", result.FlakeVerdict)
	}
	if *callCount != 1 {
		t.Errorf("callCount = %d, want 1 — flake_retries: 0 must not trigger any retry", *callCount)
	}
}

// judgeStubServer returns a stub that replies with the case's own
// trigger-marker text for every call except calls where the request's
// model field equals judgeModel, which get judgeReplyText instead — this
// lets a single stub server distinguish the primary call from the judge
// call by which model was actually requested, proving TestRunCaseJudgeUsesSeparateModelFromCaseModel's claim.
func judgeStubServer(t *testing.T, primaryReplyText, judgeModel, judgeReplyText string) (*httptest.Server, *[]string) {
	t.Helper()
	var requestedModels []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		requestedModels = append(requestedModels, req.Model)

		text := primaryReplyText
		if req.Model == judgeModel {
			text = judgeReplyText
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return srv, &requestedModels
}

func TestRunCaseJudgeAllCriteriaPass(t *testing.T) {
	srv, _ := judgeStubServer(t,
		"SKILLCI_TRIGGERED: true\nHello, thanks for reaching out!",
		"claude-opus-4-8",
		"SKILLCI_JUDGE: tone = PASS\nSKILLCI_JUDGE: length = PASS",
	)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Judge: []evalspec.JudgeCriterion{
				{Name: "tone", Criterion: "Is the response friendly?"},
				{Name: "length", Criterion: "Is the response short?"},
			},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if len(result.JudgeFindings) != 2 {
		t.Fatalf("JudgeFindings = %v, want 2 entries", result.JudgeFindings)
	}
	for _, f := range result.JudgeFindings {
		if !f.Passed {
			t.Errorf("finding %+v, want Passed=true", f)
		}
	}
}

func TestRunCaseJudgeFailureNonStrictStillPasses(t *testing.T) {
	srv, _ := judgeStubServer(t,
		"SKILLCI_TRIGGERED: true\nDo it yourself.",
		"claude-opus-4-8",
		"SKILLCI_JUDGE: tone = FAIL: response is curt and dismissive",
	)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Judge:     []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is the response friendly?"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true — non-strict judge failure must not fail the case; Failures = %v", result.Failures)
	}
	if len(result.JudgeFindings) != 1 || result.JudgeFindings[0].Passed {
		t.Fatalf("JudgeFindings = %v, want 1 failing entry", result.JudgeFindings)
	}
	if result.JudgeFindings[0].Reason != "response is curt and dismissive" {
		t.Errorf("Reason = %q, want the judge's stated reason", result.JudgeFindings[0].Reason)
	}
}

func TestRunCaseJudgeFailureStrictFails(t *testing.T) {
	srv, _ := judgeStubServer(t,
		"SKILLCI_TRIGGERED: true\nDo it yourself.",
		"claude-opus-4-8",
		"SKILLCI_JUDGE: tone = FAIL: response is curt and dismissive",
	)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:   truePtr(),
			Judge:       []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is the response friendly?"}},
			JudgeStrict: truePtr(),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — judge_strict must fail the case on a failing criterion")
	}
}

func TestRunCaseJudgeMissingJudgeModelErrors(t *testing.T) {
	srv, _ := judgeStubServer(t, "SKILLCI_TRIGGERED: true\nHi.", "", "")
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Judge:     []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is the response friendly?"}},
		},
	}

	_, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err == nil {
		t.Fatal("RunCase() error = nil, want an error — case uses judge criteria but no judge_model was passed")
	}
	if !strings.Contains(err.Error(), "judge_model") {
		t.Errorf("error = %q, want it to mention judge_model", err.Error())
	}
}

func TestRunCaseJudgeMalformedResponseLineTreatedAsFail(t *testing.T) {
	// The judge's response only returns a verdict for "tone", never
	// mentioning "length" at all — the missing criterion must be treated
	// as a fail, not silently dropped or crash the parser.
	srv, _ := judgeStubServer(t,
		"SKILLCI_TRIGGERED: true\nHi there!",
		"claude-opus-4-8",
		"SKILLCI_JUDGE: tone = PASS\nSome unrelated commentary the judge added.",
	)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Judge: []evalspec.JudgeCriterion{
				{Name: "tone", Criterion: "Is the response friendly?"},
				{Name: "length", Criterion: "Is the response short?"},
			},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if len(result.JudgeFindings) != 2 {
		t.Fatalf("JudgeFindings = %v, want 2 entries (one per configured criterion, even though the judge only answered one)", result.JudgeFindings)
	}
	var lengthFinding *JudgeFinding
	for i := range result.JudgeFindings {
		if result.JudgeFindings[i].Name == "length" {
			lengthFinding = &result.JudgeFindings[i]
		}
	}
	if lengthFinding == nil {
		t.Fatal("no finding for the length criterion at all")
	}
	if lengthFinding.Passed {
		t.Error("length finding Passed = true, want false — the judge never returned a verdict for it")
	}
}

func TestRunCaseJudgeSkippedWhenFlakeRetriesFired(t *testing.T) {
	// A case with BOTH flake_retries and judge criteria: attempt 1 fails
	// its trigger check, attempts 2-3 pass, confirmed_pass. Even though
	// the case ends up Passed=true, the judge call must never fire —
	// attempt 1's content isn't representative once retries were needed
	// (same reasoning as the snapshot guard).
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		text := "SKILLCI_TRIGGERED: false"
		if callCount >= 2 {
			text = "SKILLCI_TRIGGERED: true\nHi!"
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
	c := evalspec.Case{
		Name:   "judge-flake-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered:    truePtr(),
			FlakeRetries: intPtr(2),
			Judge:        []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is the response friendly?"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.FlakeVerdict != "confirmed_pass" {
		t.Fatalf("FlakeVerdict = %q, want confirmed_pass", result.FlakeVerdict)
	}
	if result.JudgeFindings != nil {
		t.Errorf("JudgeFindings = %v, want nil — judge must be skipped whenever a flake retry fired", result.JudgeFindings)
	}
	// Sequence is fail, pass, pass (callCount 1/2/3) — a 1-1 split after 2
	// attempts is NOT yet decided (1 remaining attempt could still tie or
	// flip it), so all 3 flake attempts are genuinely made before the
	// vote resolves to confirmed_pass. This mirrors flake-retry's own
	// TestRunCaseFlakeRetriesConfirmedPassAfterInitialFailure exactly —
	// do not "simplify" this to 2, that was verified wrong by hand.
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3 (all 3 flake attempts needed to reach a majority) — and critically, no 4th call for a judge step, since the judge must be skipped entirely", callCount)
	}
}

func TestRunCaseJudgeSkippedWhenOtherAssertionFails(t *testing.T) {
	srv, _ := judgeStubServer(t, "SKILLCI_TRIGGERED: false", "claude-opus-4-8", "SKILLCI_JUDGE: tone = PASS")
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Judge:     []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is the response friendly?"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (did not trigger as asserted)")
	}
	if result.JudgeFindings != nil {
		t.Errorf("JudgeFindings = %v, want nil — judge must be skipped when an unrelated assertion already failed", result.JudgeFindings)
	}
}

func TestRunCaseJudgeUsesSeparateModelFromCaseModel(t *testing.T) {
	srv, requestedModels := judgeStubServer(t,
		"SKILLCI_TRIGGERED: true\nHi there!",
		"claude-opus-4-8",
		"SKILLCI_JUDGE: tone = PASS",
	)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "judge-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Judge:     []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is the response friendly?"}},
		},
	}

	_, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-opus-4-8")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if len(*requestedModels) != 2 {
		t.Fatalf("requestedModels = %v, want exactly 2 calls (primary + judge)", *requestedModels)
	}
	if (*requestedModels)[0] != "claude-sonnet-5" {
		t.Errorf("first call model = %q, want claude-sonnet-5 (the case's own model)", (*requestedModels)[0])
	}
	if (*requestedModels)[1] != "claude-opus-4-8" {
		t.Errorf("second call model = %q, want claude-opus-4-8 (the configured judge model)", (*requestedModels)[1])
	}
}

func TestRunCaseFlakeRetriesNeverAppliesToBudgetAssertions(t *testing.T) {
	// The trigger assertion passes; only a budget assertion (max_tokens_loaded)
	// fails. flake_retries is set, but must have no effect — budget
	// assertions are never retried.
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: true",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{Triggered: truePtr(), MaxTokensLoaded: intPtr(10), FlakeRetries: intPtr(2)},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — max_tokens_loaded exceeded (input is 50 tokens per sequencedStubServer, budget is 10)")
	}
	if result.FlakeVerdict != "" {
		t.Errorf("FlakeVerdict = %q, want empty — a budget-only failure must never trigger a retry", result.FlakeVerdict)
	}
	if *callCount != 1 {
		t.Errorf("callCount = %d, want 1 — budget assertions are never retried regardless of flake_retries", *callCount)
	}
}
