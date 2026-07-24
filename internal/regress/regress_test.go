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

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/kabirnarang39/skillci/internal/runner"
	"github.com/kabirnarang39/skillci/internal/snapshot"
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
	}})
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
	// Regression test: a fuzz_strict case that flips on its very first run
	// (no prior history) already has its own report artifact (FuzzFindings)
	// — RunMatrix must not ALSO propose a generated eval case for the same
	// finding. Same bug class as the snapshot double-fire fix (c2e9257).
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
	if len(report.Outcomes) != 1 || report.Outcomes[0].Result.Passed {
		t.Fatalf("Outcomes = %+v, want one failed (fuzz_strict) outcome", report.Outcomes)
	}
	if len(report.GeneratedCases) != 0 {
		t.Errorf("GeneratedCases = %v, want none — fuzz cases manage their own report, not the self-growing eval loop", report.GeneratedCases)
	}
}

func TestWriteGeneratedCases(t *testing.T) {
	dir := newSkillDir(t)
	cases := []evalspec.Case{{Name: "generated-case", Prompt: "some failing prompt", SkillUnderTest: "pr-review"}}

	paths, err := WriteGeneratedCases(dir, cases)
	if err != nil {
		t.Fatalf("WriteGeneratedCases() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v, want 1", paths)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Errorf("generated file not written: %v", err)
	}
	if filepath.Dir(paths[0]) != filepath.Join(dir, "evals", "_generated") {
		t.Errorf("generated file in %s, want evals/_generated", filepath.Dir(paths[0]))
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
