package regress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/config"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/kabirnarang/skillci/internal/runner"
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
