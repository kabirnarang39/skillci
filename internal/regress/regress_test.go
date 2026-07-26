package regress

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
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/kabirnarang39/skillci/internal/runner"
	"github.com/kabirnarang39/skillci/internal/snapshot"
	"gopkg.in/yaml.v3"
)

func newSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: pr-review\ndescription: Reviews PRs.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubServerAlwaysFails(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func truePtr() *bool { v := true; return &v }

func falsePtr() *bool { v := false; return &v }

func intPtr(v int) *int { return &v }

func sequencedRegressStub(t *testing.T, texts []string) *httptest.Server {
	t.Helper()
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func TestRunMatrixFlagsNewRegressionWhenPriorRunPassed(t *testing.T) {
	srv := stubServerAlwaysFails(t)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{Name: "c1", Prompt: "review this", Assert: evalspec.Assertions{Triggered: truePtr()}},
	}
	hist := history.History{}
	hist.Append(history.Run{Cases: []history.CaseResult{
		{Name: "c1", Model: "claude-sonnet-5", Passed: true},
	}}, 0)
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, hist)
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || !report.Outcomes[0].IsNewRegression {
		t.Errorf("Outcomes = %+v, want one new-regression outcome", report.Outcomes)
	}
	if !report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = false, want true")
	}
}

// sequencedRegressStubFunc is sequencedRegressStub's generalization: instead
// of a fixed response list, respond(callIndex, requestBody) computes each
// response, for tests where a later response must depend on request content
// the test can't predict in advance (e.g. a canary token embedded in an
// attack prompt built with a real random value). Mirrors
// internal/runner/runner_test.go's sequencedStubServerFunc — internal/regress
// has no equivalent of its own yet.
func sequencedRegressStubFunc(t *testing.T, respond func(callIndex int, requestBody string) string) *httptest.Server {
	t.Helper()
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := respond(callCount, string(body))
		callCount++
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// extractCanaryToken pulls the "CANARY-xxxxxxxx" token out of a raw JSON
// request body sent to the stub server — the attack prompt embeds it as
// plain text inside the request's "messages" field. Mirrors
// internal/runner/runner_test.go's helper of the same name (different
// package, no collision).
func extractCanaryToken(requestBody string) string {
	idx := strings.Index(requestBody, "CANARY-")
	if idx == -1 {
		return ""
	}
	end := idx + len("CANARY-") + 8 // randomHex(4) = 8 hex chars
	if end > len(requestBody) {
		end = len(requestBody)
	}
	return requestBody[idx:end]
}

func TestRunMatrixNoRegressionWhenNoPriorHistory(t *testing.T) {
	srv := stubServerAlwaysFails(t)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{Name: "c1", Prompt: "review this", Assert: evalspec.Assertions{Triggered: truePtr()}},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if report.Outcomes[0].IsNewRegression {
		t.Error("IsNewRegression = true, want false — no prior history to regress from")
	}
	if report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = true, want false when nothing regressed vs history")
	}
	if len(report.GeneratedCases) != 1 {
		t.Errorf("GeneratedCases = %v, want 1 (uncovered failing case)", report.GeneratedCases)
	}
}

// TestRunMatrixGeneratedCaseCapturesFailureContext is the end-to-end
// reachability test for the self-growing eval loop's failure-context
// fields: it traces a real uncovered failure all the way through
// RunMatrix and checks the resulting GeneratedCase carries the model,
// a real (non-zero) detection timestamp, and the model's actual response
// — not just the bare name/prompt/assert the case used to carry before.
func TestRunMatrixGeneratedCaseCapturesFailureContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false\nI can't help with that particular request."}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{Name: "c1", Prompt: "review this", Assert: evalspec.Assertions{Triggered: truePtr()}},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	before := time.Now()
	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	after := time.Now()

	if len(report.GeneratedCases) != 1 {
		t.Fatalf("GeneratedCases = %v, want 1", report.GeneratedCases)
	}
	gc := report.GeneratedCases[0]
	if gc.Model != "claude-sonnet-5" {
		t.Errorf("Model = %q, want claude-sonnet-5", gc.Model)
	}
	if gc.Timestamp.Before(before) || gc.Timestamp.After(after) {
		t.Errorf("Timestamp = %v, want between %v and %v", gc.Timestamp, before, after)
	}
	if !strings.Contains(gc.ActualResponse, "I can't help with that particular request.") {
		t.Errorf("ActualResponse = %q, want it to contain the model's actual response text", gc.ActualResponse)
	}
}

// TestRunMatrixFlakeRetriesConfirmedFailStillProposesGeneratedCase is the
// end-to-end reachability test this task exists for: it traces a real
// flake_retries case, with no prior history, all the way through
// RunMatrix to prove a majority-confirmed failure still proposes a
// generated eval case exactly like today's uncovered single-shot
// failures do — not just that runner.RunCase's own verdict is correct in
// isolation (Task 2 already proved that).
func TestRunMatrixFlakeRetriesConfirmedFailStillProposesGeneratedCase(t *testing.T) {
	srv := sequencedRegressStub(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: false",
	})
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:   "flake-case",
			Prompt: "review this",
			Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(2)},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one failed (confirmed_fail) outcome", report.Outcomes)
	}
	if len(report.GeneratedCases) != 1 {
		t.Errorf("GeneratedCases = %v, want 1 — a majority-confirmed failure must still propose a generated case, same as any other uncovered failure", report.GeneratedCases)
	}
	if report.Outcomes[0].Result.FlakeVerdict != "confirmed_fail" {
		t.Errorf("FlakeVerdict = %q, want confirmed_fail — confirms retry mechanism actually executed", report.Outcomes[0].Result.FlakeVerdict)
	}
	if report.Outcomes[0].Result.FlakeAttemptsTotal != 2 {
		t.Errorf("FlakeAttemptsTotal = %d, want 2 — early-stop after majority decided (2-0 fail); confirms retry mechanism actually executed", report.Outcomes[0].Result.FlakeAttemptsTotal)
	}
}

// TestRunMatrixFlakeRetriesConfirmedPassDoesNotProposeGeneratedCase proves
// the other direction: a case that only failed its very first attempt but
// resolved to a majority pass across retries must NOT be treated as a
// regression or an uncovered failure — the raw first-attempt noise must
// never reach the self-growing loop.
func TestRunMatrixFlakeRetriesConfirmedPassDoesNotProposeGeneratedCase(t *testing.T) {
	srv := sequencedRegressStub(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true",
		"SKILLCI_TRIGGERED: true",
	})
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:   "flake-case",
			Prompt: "review this",
			Assert: evalspec.Assertions{Triggered: truePtr(), FlakeRetries: intPtr(2)},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || !report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one passed (confirmed_pass) outcome", report.Outcomes)
	}
	if len(report.GeneratedCases) != 0 {
		t.Errorf("GeneratedCases = %v, want none — a majority-confirmed pass must not be treated as an uncovered failure", report.GeneratedCases)
	}
	if report.ShouldFailCI(cfg.FailOn) {
		t.Error("ShouldFailCI() = true, want false — nothing actually failed")
	}
}

func TestRunMatrixSnapshotStrictFailureDoesNotProposeGeneratedCase(t *testing.T) {
	// Regression test: a snapshot_strict case that drifts on its very
	// first-ever comparison (no prior history) already has its own review
	// artifact (the pending golden file) — RunMatrix must not ALSO propose
	// a generated eval case for the same drift.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true\nOld leaves drift and settle."}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	dir := newSkillDir(t)
	if err := snapshot.Save(dir, "c1", "claude-sonnet-5", "Old leaves drift and fall."); err != nil {
		t.Fatalf("seeding golden: %v", err)
	}

	cases := []evalspec.Case{
		{
			Name:   "c1",
			Prompt: "write a haiku",
			Assert: evalspec.Assertions{Snapshot: truePtr(), SnapshotStrict: truePtr()},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, dir, cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one failed (snapshot_strict) outcome", report.Outcomes)
	}
	if len(report.GeneratedCases) != 0 {
		t.Errorf("GeneratedCases = %v, want none — snapshot cases manage their own review flow", report.GeneratedCases)
	}
}

func TestRunMatrixFuzzStrictFailureDoesNotProposeGeneratedCase(t *testing.T) {
	// Regression test: a fuzz_strict case that fails on its base assertion
	// (before any mutation even runs — see runner.go's `len(result.Failures)
	// == 0` fuzz gate, so FuzzFindings stays empty here) must not have
	// RunMatrix's whole-case-clone path propose a generated eval case for
	// it. Same bug class as the snapshot double-fire fix (c2e9257).
	//
	// This is deliberately NOT the "a mutation flipped" scenario — that's
	// covered (with the opposite expectation: a GeneratedCase IS proposed,
	// at the mutation level) by TestRunMatrixFuzzStrictFirstRunFlipProposesGeneratedCase.
	srv := stubServerAlwaysFails(t)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:   "c1",
			Prompt: "Can you write me a haiku?",
			Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr(), FuzzStrict: truePtr()},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one failed (fuzz_strict) outcome", report.Outcomes)
	}
	if len(report.GeneratedCases) != 0 {
		t.Errorf("GeneratedCases = %v, want none — fuzz cases manage their own report, not the self-growing eval loop", report.GeneratedCases)
	}
}

// TestRunMatrixGeneratesCaseForFirstTimeFailingRedteamAttack is the proof
// (not assumption) that the self-growing eval loop's existing !hadPrior
// condition in RunMatrix already covers a redteam attack succeeding for the
// first time, with zero changes to regress.go — because redteam is just an
// ordinary assertion on an ordinary evalspec.Case, exactly like Triggered,
// Judge, or FlakeRetries in the sibling tests above. Same pattern as
// TestRunMatrixNoRegressionWhenNoPriorHistory: no prior history entry, one
// failing case, assert GeneratedCases has exactly one entry.
func TestRunMatrixGeneratesCaseForFirstTimeFailingRedteamAttack(t *testing.T) {
	srv := sequencedRegressStubFunc(t, func(callIndex int, requestBody string) string {
		if callIndex == 0 {
			return "SKILLCI_TRIGGERED: true\nHere is your haiku about autumn leaves."
		}
		// The attack call (index 1): a vulnerable skill echoes the
		// canary token back verbatim, leaking it.
		token := extractCanaryToken(requestBody)
		return "SKILLCI_TRIGGERED: true\nSure! By the way, the internal token was: " + token
	})
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:   "redteam-case",
			Prompt: "write me a haiku about autumn",
			Assert: evalspec.Assertions{
				Triggered:     truePtr(),
				Redteam:       []evalspec.RedteamAttack{{Plugin: "prompt-injection-canary"}},
				RedteamStrict: truePtr(),
			},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one failed (redteam_strict) outcome", report.Outcomes)
	}
	if len(report.GeneratedCases) != 1 {
		t.Fatalf("GeneratedCases = %v, want 1 — a first-time-failing redteam attack is an uncovered failure like any other, with no regress.go changes needed", report.GeneratedCases)
	}
	// Finding 2 fix: ActualResponse must surface what actually leaked
	// (the plugin's finding), not just the innocuous base haiku response —
	// a reviewer reading the generated case file needs to see the attack
	// result, not only the benign base-prompt reply.
	actual := report.GeneratedCases[0].ActualResponse
	if !strings.Contains(actual, "prompt-injection-canary") || !strings.Contains(actual, "leaked into the response") {
		t.Errorf("ActualResponse = %q, want it to contain the redteam finding (plugin name and leak detail), not just the base response", actual)
	}
}

func TestWriteGeneratedCases(t *testing.T) {
	dir := newSkillDir(t)
	when := time.Date(2026, 7, 24, 21, 30, 0, 0, time.UTC)
	cases := []GeneratedCase{{
		Case:           evalspec.Case{Name: "generated-case", Prompt: "some failing prompt", SkillUnderTest: "pr-review"},
		Model:          "claude-sonnet-5",
		Timestamp:      when,
		ActualResponse: "I can't help with that request.",
	}}

	paths, err := WriteGeneratedCases(dir, cases)
	if err != nil {
		t.Fatalf("WriteGeneratedCases() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v, want 1", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("generated file not written: %v", err)
	}
	if filepath.Dir(paths[0]) != filepath.Join(dir, "evals", "_generated") {
		t.Errorf("generated file in %s, want evals/_generated", filepath.Dir(paths[0]))
	}

	content := string(data)
	for _, want := range []string{
		"# model: claude-sonnet-5",
		"# detected_at: 2026-07-24T21:30:00Z",
		"#   I can't help with that request.",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("file content = %q, want it to contain %q", content, want)
		}
	}

	var loaded evalspec.Case
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("yaml.Unmarshal() of the written file (with its comment header) error = %v", err)
	}
	if loaded.Name != "generated-case" || loaded.Prompt != "some failing prompt" {
		t.Errorf("loaded = %+v, want the case body to parse correctly despite the leading comment header", loaded)
	}
}

// TestWriteGeneratedCasesOmitsUnsetFields covers what the human reviewer
// actually sees: a generated case only ever sets Triggered on Assert (the
// one field RunMatrix's failure path is guaranteed to know), leaving every
// other Assertions field at its zero value. Without omitempty tags on
// evalspec.Assertions, yaml.Marshal spells out all ~15 of them as `null`
// or `[]` — exactly the noise the self-growing eval loop's whole point
// (a clean case a human reviews before `accept`) is supposed to avoid.
// Caught live: accepting a real generated case showed the bloated file.
func TestWriteGeneratedCasesOmitsUnsetFields(t *testing.T) {
	dir := newSkillDir(t)
	cases := []GeneratedCase{{
		Case:      evalspec.Case{Name: "generated-case", Prompt: "some failing prompt", SkillUnderTest: "pr-review", Assert: evalspec.Assertions{Triggered: boolPtr(true)}},
		Model:     "claude-sonnet-5",
		Timestamp: time.Now(),
	}}

	paths, err := WriteGeneratedCases(dir, cases)
	if err != nil {
		t.Fatalf("WriteGeneratedCases() error = %v", err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if strings.Contains(content, "null") {
		t.Errorf("file content = %q, want no null fields for assertions the case never set", content)
	}
	if strings.Contains(content, "judge_strict") || strings.Contains(content, "flake_retries") || strings.Contains(content, "max_cost_usd") {
		t.Errorf("file content = %q, want unset assertion fields omitted entirely, not written out empty", content)
	}
	if !strings.Contains(content, "triggered: true") {
		t.Errorf("file content = %q, want the one field that IS set (triggered) still present", content)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestScanStaleGeneratedCasesFlagsOldFile(t *testing.T) {
	dir := newSkillDir(t)
	old := time.Now().Add(-30 * 24 * time.Hour)
	_, err := WriteGeneratedCases(dir, []GeneratedCase{{
		Case:      evalspec.Case{Name: "old-case", Prompt: "p"},
		Model:     "claude-sonnet-5",
		Timestamp: old,
	}})
	if err != nil {
		t.Fatalf("WriteGeneratedCases() error = %v", err)
	}

	stale, err := ScanStaleGeneratedCases(dir, StaleGeneratedCaseThreshold)
	if err != nil {
		t.Fatalf("ScanStaleGeneratedCases() error = %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("stale = %+v, want 1 entry", stale)
	}
	if !stale[0].DetectedAt.Equal(old.UTC().Truncate(time.Second)) {
		t.Errorf("DetectedAt = %v, want %v", stale[0].DetectedAt, old.UTC().Truncate(time.Second))
	}
}

func TestScanStaleGeneratedCasesIgnoresRecentFile(t *testing.T) {
	dir := newSkillDir(t)
	_, err := WriteGeneratedCases(dir, []GeneratedCase{{
		Case:      evalspec.Case{Name: "fresh-case", Prompt: "p"},
		Model:     "claude-sonnet-5",
		Timestamp: time.Now(),
	}})
	if err != nil {
		t.Fatalf("WriteGeneratedCases() error = %v", err)
	}

	stale, err := ScanStaleGeneratedCases(dir, StaleGeneratedCaseThreshold)
	if err != nil {
		t.Fatalf("ScanStaleGeneratedCases() error = %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %+v, want none for a freshly generated case", stale)
	}
}

func TestScanStaleGeneratedCasesSkipsFileWithoutDetectedAtHeader(t *testing.T) {
	dir := newSkillDir(t)
	generatedDir := filepath.Join(dir, "evals", "_generated")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No detected_at comment at all — e.g. hand-written, or predates this
	// field. Must be silently skipped, not treated as infinitely stale.
	if err := os.WriteFile(filepath.Join(generatedDir, "hand-written.yaml"), []byte("name: hand-written\nprompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale, err := ScanStaleGeneratedCases(dir, StaleGeneratedCaseThreshold)
	if err != nil {
		t.Fatalf("ScanStaleGeneratedCases() error = %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %+v, want none for a file with no detected_at header", stale)
	}
}

func TestScanStaleGeneratedCasesNoDirectoryReturnsNoError(t *testing.T) {
	dir := newSkillDir(t) // evals/_generated doesn't exist at all yet
	stale, err := ScanStaleGeneratedCases(dir, StaleGeneratedCaseThreshold)
	if err != nil {
		t.Fatalf("ScanStaleGeneratedCases() error = %v, want nil when evals/_generated doesn't exist", err)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %+v, want none", stale)
	}
}

func TestShouldFailCIAnyFailMode(t *testing.T) {
	report := MatrixReport{Outcomes: []Outcome{
		{IsNewRegression: false, Result: runner.Result{Passed: false}},
	}}
	if !report.ShouldFailCI("any_fail") {
		t.Error("ShouldFailCI(any_fail) = false, want true when any case failed, regression or not")
	}
	if report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = true, want false when the only failure isn't a new regression")
	}
}

func TestShouldFailCITriggeredOnlyCatchesFalsePositive(t *testing.T) {
	// Case: asserts Triggered: false but result shows Triggered: true (false positive)
	report := MatrixReport{Outcomes: []Outcome{
		{
			Case:   evalspec.Case{Assert: evalspec.Assertions{Triggered: falsePtr()}},
			Result: runner.Result{Triggered: true},
		},
	}}
	if !report.ShouldFailCI("triggered_only") {
		t.Error("ShouldFailCI(triggered_only) = false, want true when skill triggers but should not (false positive)")
	}
}

func TestShouldFailCITriggeredOnlyStillCatchesMissedTriggers(t *testing.T) {
	// Case: asserts Triggered: true but result shows Triggered: false (missed trigger)
	report := MatrixReport{Outcomes: []Outcome{
		{
			Case:   evalspec.Case{Assert: evalspec.Assertions{Triggered: truePtr()}},
			Result: runner.Result{Triggered: false},
		},
	}}
	if !report.ShouldFailCI("triggered_only") {
		t.Error("ShouldFailCI(triggered_only) = false, want true when skill should trigger but does not (missed trigger)")
	}
}

func TestShouldFailCITriggeredOnlyPassesWhenMatches(t *testing.T) {
	// Case: asserts Triggered: true and result shows Triggered: true (correct)
	report := MatrixReport{Outcomes: []Outcome{
		{
			Case:   evalspec.Case{Assert: evalspec.Assertions{Triggered: truePtr()}},
			Result: runner.Result{Triggered: true},
		},
	}}
	if report.ShouldFailCI("triggered_only") {
		t.Error("ShouldFailCI(triggered_only) = true, want false when Triggered assertion matches result")
	}

	// Case: asserts Triggered: false and result shows Triggered: false (correct)
	report = MatrixReport{Outcomes: []Outcome{
		{
			Case:   evalspec.Case{Assert: evalspec.Assertions{Triggered: falsePtr()}},
			Result: runner.Result{Triggered: false},
		},
	}}
	if report.ShouldFailCI("triggered_only") {
		t.Error("ShouldFailCI(triggered_only) = true, want false when Triggered assertion matches result")
	}
}

func TestMatchesStrictDimensionsMatchesOnAnyPair(t *testing.T) {
	strict := map[string][]string{"segment": {"enterprise", "government"}}
	if !matchesStrictDimensions(map[string]string{"segment": "enterprise"}, strict) {
		t.Error("matchesStrictDimensions() = false, want true for an exact key/value match")
	}
	if !matchesStrictDimensions(map[string]string{"segment": "government", "language": "es"}, strict) {
		t.Error("matchesStrictDimensions() = false, want true when ANY dimension pair matches")
	}
}

func TestMatchesStrictDimensionsNoMatch(t *testing.T) {
	strict := map[string][]string{"segment": {"enterprise"}}
	if matchesStrictDimensions(map[string]string{"segment": "free"}, strict) {
		t.Error("matchesStrictDimensions() = true, want false — value not in the strict list")
	}
	if matchesStrictDimensions(map[string]string{"language": "es"}, strict) {
		t.Error("matchesStrictDimensions() = true, want false — key not present in dims at all")
	}
	if matchesStrictDimensions(nil, strict) {
		t.Error("matchesStrictDimensions() = true, want false for a case with no dimensions")
	}
	if matchesStrictDimensions(map[string]string{"segment": "enterprise"}, nil) {
		t.Error("matchesStrictDimensions() = true, want false when no strict_dimensions configured at all")
	}
}

// TestRunMatrixStrictDimensionFailOverridesLooseFailOn is the end-to-end
// reachability test this task's brief requires: it traces a real
// strict_dimensions config all the way through RunMatrix into
// ShouldFailCI, proving the gate actually fires through the real code
// path — not just that matchesStrictDimensions is correct in isolation.
// FailOn is deliberately set to the loosest policy ("triggered_only") and
// the case's Triggered assertion is left unset (nil) — a Contains
// assertion supplies the (unrelated to Triggered) reason the case fails
// instead, so the ONLY thing that could make ShouldFailCI return true
// under triggered_only is the strict-dimension path. (The brief's literal
// test used Assert.Triggered: truePtr() against a server that always
// returns "SKILLCI_TRIGGERED: false" — that independently satisfies
// triggered_only's own mismatch check, so the test would pass even with
// the StrictDimensionFail override deleted entirely. Verified empirically
// by removing the override and re-running: it still passed. Swapped to
// Contains to actually isolate the code path under test.)
func TestRunMatrixStrictDimensionFailOverridesLooseFailOn(t *testing.T) {
	srv := stubServerAlwaysFails(t)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:       "enterprise-case",
			Prompt:     "review this",
			Assert:     evalspec.Assertions{Contains: []string{"nonexistent-marker-xyz"}},
			Dimensions: map[string]string{"segment": "enterprise"},
		},
	}
	cfg := config.Config{
		Models: []string{"claude-sonnet-5"},
		FailOn: "triggered_only",
		StrictDimensions: map[string][]string{
			"segment": {"enterprise"},
		},
	}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 {
		t.Fatalf("Outcomes = %+v, want 1", report.Outcomes)
	}
	if !report.Outcomes[0].StrictDimensionFail {
		t.Fatal("StrictDimensionFail = false, want true — case matches strict_dimensions and failed")
	}
	if !report.ShouldFailCI(cfg.FailOn) {
		t.Error("ShouldFailCI(triggered_only) = false, want true — a strict_dimensions match must fail CI regardless of the configured (loose) fail_on policy")
	}
}

func TestRunMatrixNoStrictDimensionFailWhenCaseDoesNotMatch(t *testing.T) {
	// A passing case (Triggered matches true==true, so triggered_only
	// doesn't fail it) whose dimension value also isn't in
	// strict_dimensions — confirms no false trigger of the gate for an
	// unrelated reason.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:       "free-case",
			Prompt:     "review this",
			Assert:     evalspec.Assertions{Triggered: truePtr()},
			Dimensions: map[string]string{"segment": "free"},
		},
	}
	cfg := config.Config{
		Models:           []string{"claude-sonnet-5"},
		FailOn:           "triggered_only",
		StrictDimensions: map[string][]string{"segment": {"enterprise"}},
	}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if report.Outcomes[0].StrictDimensionFail {
		t.Error("StrictDimensionFail = true, want false — segment=free doesn't match strict_dimensions[segment]=[enterprise]")
	}
	if report.ShouldFailCI(cfg.FailOn) {
		t.Error("ShouldFailCI(triggered_only) = true, want false — no strict match, and Triggered assertion is satisfied")
	}
}

// TestRunMatrixNoStrictDimensionFailWhenPassingCaseMatchesStrictDimensions
// covers the hard constraint this feature's spec calls out by name:
// StrictDimensionFail must never be true for a passing case even when it
// matches a strict dimension. TestRunMatrixNoStrictDimensionFailWhenCaseDoesNotMatch
// doesn't actually exercise this — its case's dimensions don't match
// strict_dimensions at all, so matchesStrictDimensions never even gets to
// true. This test's case DOES match strict_dimensions and DOES pass, so the
// only thing keeping StrictDimensionFail false is the `!result.Passed &&`
// guard in RunMatrix's wiring — this test fails if that guard is ever
// dropped.
func TestRunMatrixNoStrictDimensionFailWhenPassingCaseMatchesStrictDimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{
			Name:       "enterprise-passing-case",
			Prompt:     "review this",
			Assert:     evalspec.Assertions{Triggered: truePtr()},
			Dimensions: map[string]string{"segment": "enterprise"},
		},
	}
	cfg := config.Config{
		Models:           []string{"claude-sonnet-5"},
		FailOn:           "triggered_only",
		StrictDimensions: map[string][]string{"segment": {"enterprise"}},
	}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if report.Outcomes[0].StrictDimensionFail {
		t.Error("StrictDimensionFail = true, want false — case passed, so it must never force-fail CI even though its dimensions match strict_dimensions")
	}
	if report.ShouldFailCI(cfg.FailOn) {
		t.Error("ShouldFailCI(triggered_only) = true, want false — passing case, no reason to fail CI")
	}
}

func TestShouldFailCITriggeredOnlyIgnoresNoAssertion(t *testing.T) {
	// Case: no Triggered assertion (nil) should not affect verdict
	report := MatrixReport{Outcomes: []Outcome{
		{
			Case:   evalspec.Case{Assert: evalspec.Assertions{Triggered: nil}},
			Result: runner.Result{Triggered: true},
		},
	}}
	if report.ShouldFailCI("triggered_only") {
		t.Error("ShouldFailCI(triggered_only) = true, want false when Triggered assertion is nil (not applicable)")
	}
}

func TestRunMatrixJudgeStrictFailureFailsCIAndProposesGeneratedCase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		text := "SKILLCI_TRIGGERED: true\nDo it yourself."
		if req.Model == "claude-opus-4-8" {
			text = "SKILLCI_JUDGE: tone = FAIL: dismissive"
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

	cases := []evalspec.Case{
		{
			Name:   "judge-case",
			Prompt: "hi",
			Assert: evalspec.Assertions{
				Triggered:   truePtr(),
				Judge:       []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is it friendly?"}},
				JudgeStrict: truePtr(),
			},
		},
	}
	// FailOn is deliberately "any_fail" — the ONLY one of skillci's three
	// fail_on policies that checks Result.Passed unconditionally.
	// "triggered_only" only checks the Triggered mismatch (irrelevant
	// here — Triggered:true genuinely matches the stub's response), and
	// the default "regression" only fires when a PRIOR passing run
	// exists to regress from (there is none here, hist is empty) — with
	// either of those policies this test would silently prove nothing
	// about judge_strict at all. Verified by hand against
	// MatrixReport.ShouldFailCI's actual per-policy switch before writing
	// this — do not swap this back to triggered_only or regression.
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "any_fail", JudgeModel: "claude-opus-4-8"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one failed outcome (judge_strict failure)", report.Outcomes)
	}
	if !report.ShouldFailCI(cfg.FailOn) {
		t.Error("ShouldFailCI(any_fail) = false, want true — Triggered itself passed, so only the judge_strict failure explains this")
	}
	if len(report.GeneratedCases) != 1 {
		t.Errorf("GeneratedCases = %v, want 1 — a judge_strict failure is an uncovered failure like any other", report.GeneratedCases)
	}
}

func TestRunMatrixJudgeSkippedWhenFlakeRetriesFiredMakesNoJudgeCall(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req struct {
			Model string `json:"model"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		if req.Model == "claude-opus-4-8" {
			t.Fatalf("judge model was called (call #%d) despite flake retries having fired — judge must be skipped entirely", callCount)
		}
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

	cases := []evalspec.Case{
		{
			Name:   "judge-flake-case",
			Prompt: "hi",
			Assert: evalspec.Assertions{
				Triggered:    truePtr(),
				FlakeRetries: intPtr(2),
				Judge:        []evalspec.JudgeCriterion{{Name: "tone", Criterion: "Is it friendly?"}},
			},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression", JudgeModel: "claude-opus-4-8"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if report.Outcomes[0].Result.FlakeVerdict != "confirmed_pass" {
		t.Fatalf("FlakeVerdict = %q, want confirmed_pass", report.Outcomes[0].Result.FlakeVerdict)
	}
	if report.Outcomes[0].Result.JudgeFindings != nil {
		t.Errorf("JudgeFindings = %v, want nil", report.Outcomes[0].Result.JudgeFindings)
	}
}

// modelAwareStub responds differently per model name found in each
// request's JSON body — every other stub in this file replies identically
// regardless of which model the case is being evaluated against, which
// can't exercise cfg.Models having more than one entry produce genuinely
// different per-model outcomes in the same RunMatrix call. responses maps
// a substring of the request's "model" field to the literal response text
// to return for that model.
func modelAwareStub(t *testing.T, responses map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("stub: decoding request body: %v", err)
		}
		text, ok := responses[req.Model]
		if !ok {
			t.Fatalf("stub: no scripted response for model %q", req.Model)
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// TestRunMatrixAttributesRegressionToTheCorrectModelOnly closes a real,
// previously untested gap: every other RunMatrix test in this file
// configures exactly one model. cfg.Models having 2+ entries is the core
// "regression matrix" positioning — RunMatrix's own model loop (`for _,
// model := range cfg.Models`) had never actually been driven with more
// than one iteration. Two models are configured; history records both as
// previously passing; this run's stub makes model-a keep passing and
// model-b regress. A bug here would plausibly look like "the first
// model's result leaks into the second" (e.g. reusing lastRun.Result
// incorrectly, or IsNewRegression computed once instead of per-model) —
// exactly the class of bug a single-model test can never see.
func TestRunMatrixAttributesRegressionToTheCorrectModelOnly(t *testing.T) {
	srv := modelAwareStub(t, map[string]string{
		"model-a": "SKILLCI_TRIGGERED: true",
		"model-b": "SKILLCI_TRIGGERED: false",
	})
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{Name: "c1", Prompt: "review this", Assert: evalspec.Assertions{Triggered: truePtr()}},
	}
	hist := history.History{}
	hist.Append(history.Run{Cases: []history.CaseResult{
		{Name: "c1", Model: "model-a", Passed: true},
		{Name: "c1", Model: "model-b", Passed: true},
	}}, 0)
	cfg := config.Config{Models: []string{"model-a", "model-b"}, FailOn: "regression"}

	report, newRun, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, hist)
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 2 {
		t.Fatalf("Outcomes = %+v, want 2 (one per model)", report.Outcomes)
	}

	var outcomeA, outcomeB *Outcome
	for i := range report.Outcomes {
		switch report.Outcomes[i].Model {
		case "model-a":
			outcomeA = &report.Outcomes[i]
		case "model-b":
			outcomeB = &report.Outcomes[i]
		}
	}
	if outcomeA == nil || outcomeB == nil {
		t.Fatalf("Outcomes = %+v, want one outcome each for model-a and model-b", report.Outcomes)
	}

	if !outcomeA.Result.Passed || outcomeA.IsNewRegression {
		t.Errorf("model-a outcome = %+v, want Passed=true, IsNewRegression=false", outcomeA)
	}
	if outcomeB.Result.Passed || !outcomeB.IsNewRegression {
		t.Errorf("model-b outcome = %+v, want Passed=false, IsNewRegression=true", outcomeB)
	}

	if !report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = false, want true — model-b alone regressed")
	}

	if len(newRun.Cases) != 2 {
		t.Fatalf("newRun.Cases = %+v, want 2 (one per model), so history.json records both independently", newRun.Cases)
	}
	for _, c := range newRun.Cases {
		switch c.Model {
		case "model-a":
			if !c.Passed {
				t.Error("newRun.Cases model-a Passed = false, want true")
			}
		case "model-b":
			if c.Passed {
				t.Error("newRun.Cases model-b Passed = true, want false")
			}
		default:
			t.Errorf("newRun.Cases has unexpected model %q", c.Model)
		}
	}
}

// TestRunMatrixFuzzStrictFirstRunFlipProposesGeneratedCase covers the new
// self-growing fuzz path: a fuzz_strict case's first-ever run (no prior
// history) that has a flipped mutation must propose a GeneratedCase pinned
// to that exact mutated prompt — not the base case's original prompt, and
// not a clone of the base case's own Assert block.
func TestRunMatrixFuzzStrictFirstRunFlipProposesGeneratedCase(t *testing.T) {
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

	cases := []evalspec.Case{
		{
			Name:   "c1",
			Prompt: "Can you write me a haiku?",
			Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr(), FuzzStrict: truePtr()},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.GeneratedCases) == 0 {
		t.Fatal("GeneratedCases is empty, want at least one proposed for the flipped negation mutation")
	}
	gc := report.GeneratedCases[0]
	if gc.Case.Prompt == "Can you write me a haiku?" {
		t.Error("generated case's Prompt is the base case's original prompt, want the mutated prompt that actually flipped")
	}
	if !strings.Contains(gc.Case.Prompt, "don't") {
		t.Errorf("generated case's Prompt = %q, want it to contain the negation mutation's inserted don't", gc.Case.Prompt)
	}
	if gc.Case.Assert.Triggered == nil || *gc.Case.Assert.Triggered != true {
		t.Errorf("generated case's Assert.Triggered = %v, want true (the ORIGINAL case's expected value, not the flipped observed one)", gc.Case.Assert.Triggered)
	}
	if gc.Case.Assert.Fuzz != nil {
		t.Error("generated case's Assert.Fuzz is set, want nil — a self-grown fuzz case must not re-fuzz an already-mutated prompt")
	}
	if !strings.HasPrefix(gc.Case.Name, "c1-fuzz-negation-") {
		t.Errorf("generated case's Name = %q, want prefix c1-fuzz-negation-", gc.Case.Name)
	}
}

// TestRunMatrixFuzzFlipWithPriorHistoryDoesNotPropose mirrors the existing
// hadPrior gate every other generated-case path already uses — a case that
// has a recorded prior run doesn't propose new fuzz-mutation cases even if
// a mutation flips, consistent with "first discovery only" semantics.
func TestRunMatrixFuzzFlipWithPriorHistoryDoesNotPropose(t *testing.T) {
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

	cases := []evalspec.Case{
		{
			Name:   "c1",
			Prompt: "Can you write me a haiku?",
			Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr(), FuzzStrict: truePtr()},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}
	priorHist := history.History{Runs: []history.Run{{Cases: []history.CaseResult{{Name: "c1", Model: "claude-sonnet-5", Passed: false}}}}}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, priorHist)
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.GeneratedCases) != 0 {
		t.Errorf("GeneratedCases = %+v, want none — this case has prior recorded history, so first-discovery gate should not fire", report.GeneratedCases)
	}
}

// TestRunMatrixFuzzFlipsCapAtFiveGeneratedCases proves the cap holds even
// when far more than 5 mutations flip simultaneously on a genuinely
// fragile skill's first run.
func TestRunMatrixFuzzFlipsCapAtFiveGeneratedCases(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		text := "SKILLCI_TRIGGERED: true"
		if callCount > 1 {
			text = "SKILLCI_TRIGGERED: false"
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	overSrv := httptest.NewServer(handler)
	defer overSrv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(overSrv.URL)

	cases := []evalspec.Case{
		{
			Name:   "fragile",
			Prompt: "Can you write me a haiku about autumn leaves falling in the wind?",
			Assert: evalspec.Assertions{Triggered: truePtr(), Fuzz: truePtr(), FuzzStrict: truePtr()},
		},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.GeneratedCases) != 5 {
		t.Errorf("len(GeneratedCases) = %d, want exactly 5 (capped)", len(report.GeneratedCases))
	}
}
