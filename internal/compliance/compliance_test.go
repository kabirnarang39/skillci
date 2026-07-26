package compliance

import (
	"strings"
	"testing"
	"time"

	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/history"
)

func boolPtr(b bool) *bool { return &b }

func TestGatherCountsAssertionTypesAcrossCases(t *testing.T) {
	cases := []evalspec.Case{
		{
			Name: "triggered-and-judge",
			Assert: evalspec.Assertions{
				Triggered: boolPtr(true),
				Judge: []evalspec.JudgeCriterion{
					{Name: "tone", Criterion: "is polite"},
					{Name: "accuracy", Criterion: "is correct"},
				},
			},
		},
		{
			Name: "redteam-and-fuzz",
			Assert: evalspec.Assertions{
				Redteam: []evalspec.RedteamAttack{
					{Plugin: "prompt-injection-canary"},
					{Plugin: "instruction-leakage"},
				},
				Fuzz: boolPtr(true),
			},
		},
		{
			Name: "snapshot-and-budget",
			Assert: evalspec.Assertions{
				Snapshot:     boolPtr(true),
				MaxLatencyMs: int64Ptr(500),
			},
		},
	}

	ev := Gather(cases, history.History{})

	if ev.TotalCases != 3 {
		t.Errorf("TotalCases = %d, want 3", ev.TotalCases)
	}
	if ev.CasesWithTriggered != 1 {
		t.Errorf("CasesWithTriggered = %d, want 1", ev.CasesWithTriggered)
	}
	if ev.CasesWithJudge != 1 || ev.TotalJudgeCriteria != 2 {
		t.Errorf("CasesWithJudge = %d, TotalJudgeCriteria = %d, want 1, 2", ev.CasesWithJudge, ev.TotalJudgeCriteria)
	}
	if ev.CasesWithRedteam != 1 || len(ev.RedteamPlugins) != 2 {
		t.Errorf("CasesWithRedteam = %d, RedteamPlugins = %v, want 1, 2 plugins", ev.CasesWithRedteam, ev.RedteamPlugins)
	}
	if ev.RedteamPlugins[0] != "instruction-leakage" || ev.RedteamPlugins[1] != "prompt-injection-canary" {
		t.Errorf("RedteamPlugins = %v, want sorted [instruction-leakage prompt-injection-canary]", ev.RedteamPlugins)
	}
	if ev.CasesWithFuzz != 1 {
		t.Errorf("CasesWithFuzz = %d, want 1", ev.CasesWithFuzz)
	}
	if ev.CasesWithSnapshot != 1 {
		t.Errorf("CasesWithSnapshot = %d, want 1", ev.CasesWithSnapshot)
	}
	if ev.CasesWithBudget != 1 {
		t.Errorf("CasesWithBudget = %d, want 1", ev.CasesWithBudget)
	}
}

func int64Ptr(i int64) *int64 { return &i }

func TestGatherNoRunsReportsHasRunsFalse(t *testing.T) {
	ev := Gather(nil, history.History{})
	if ev.HasRuns {
		t.Error("HasRuns = true, want false for empty history")
	}
	if ev.TotalRuns != 0 {
		t.Errorf("TotalRuns = %d, want 0", ev.TotalRuns)
	}
}

func TestGatherRunsReportsOldestNewestAndModels(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	hist := history.History{Runs: []history.Run{
		{Timestamp: oldest, Cases: []history.CaseResult{{Name: "c1", Model: "claude-sonnet-5", Passed: true}}},
		{Timestamp: newest, Cases: []history.CaseResult{{Name: "c1", Model: "claude-opus-4-8", Passed: true}}},
	}}

	ev := Gather(nil, hist)

	if !ev.HasRuns || ev.TotalRuns != 2 {
		t.Fatalf("HasRuns = %v, TotalRuns = %d, want true, 2", ev.HasRuns, ev.TotalRuns)
	}
	if !ev.OldestRun.Equal(oldest) {
		t.Errorf("OldestRun = %v, want %v", ev.OldestRun, oldest)
	}
	if !ev.NewestRun.Equal(newest) {
		t.Errorf("NewestRun = %v, want %v", ev.NewestRun, newest)
	}
	if len(ev.ModelsCovered) != 2 || ev.ModelsCovered[0] != "claude-opus-4-8" || ev.ModelsCovered[1] != "claude-sonnet-5" {
		t.Errorf("ModelsCovered = %v, want sorted [claude-opus-4-8 claude-sonnet-5]", ev.ModelsCovered)
	}
}

// TestRenderNISTAIRMFIncludesDisclaimerAndRealCounts is the reachability
// test proving Render actually surfaces Evidence's real numbers in the
// output text, not just that it doesn't crash.
func TestRenderNISTAIRMFIncludesDisclaimerAndRealCounts(t *testing.T) {
	ev := Evidence{
		TotalCases:         5,
		CasesWithTriggered: 3,
		CasesWithJudge:     2,
		TotalJudgeCriteria: 4,
		HasRuns:            true,
		TotalRuns:          10,
		OldestRun:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NewestRun:          time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	out := RenderNISTAIRMF(ev, "path/to/skill")

	if !strings.Contains(out, "evidence, not certification") {
		t.Error("output missing the evidence-not-certification disclaimer")
	}
	if !strings.Contains(out, "5 eval case(s)") {
		t.Errorf("output = %q, want it to report 5 eval cases", out)
	}
	if !strings.Contains(out, "3 case(s) have an explicit `triggered`") {
		t.Errorf("output = %q, want it to report 3 triggered cases", out)
	}
	if !strings.Contains(out, "2 case(s) use `judge:` criteria (4 total criteria)") {
		t.Errorf("output = %q, want it to report judge criteria counts", out)
	}
	if !strings.Contains(out, "MEASURE 2.1") || !strings.Contains(out, "MEASURE 2.6") || !strings.Contains(out, "MEASURE 2.7") {
		t.Error("output missing expected MEASURE subcategory headings")
	}
}

// TestRenderNISTAIRMFNoRunsPointsToRegress covers the zero-history case:
// the report must say how to start building evidence, not silently print
// a blank MEASURE 2.6 section.
func TestRenderNISTAIRMFNoRunsPointsToRegress(t *testing.T) {
	out := RenderNISTAIRMF(Evidence{}, "path/to/skill")
	if !strings.Contains(out, "skillci regress") {
		t.Errorf("output = %q, want a pointer to `skillci regress` when there's no history yet", out)
	}
}

// TestRenderEUAIActSurfacesRetentionCapHonestly is the reachability test
// for the report's key honesty requirement: it must state the actual
// retention cap and explicitly flag that a run-count cap isn't the same
// as the Act's wall-clock six-month minimum, rather than implying the
// default satisfies it unconditionally.
func TestRenderEUAIActSurfacesRetentionCapHonestly(t *testing.T) {
	out := RenderEUAIAct(Evidence{}, "path/to/skill", 0)

	if !strings.Contains(out, "Article 11") || !strings.Contains(out, "Article 12") {
		t.Error("output missing expected Article 11/12 headings")
	}
	if !strings.Contains(out, "at least six months") {
		t.Error("output missing the Article 19/26 six-month retention quote")
	}
	if !strings.Contains(out, "200 run(s)") {
		t.Errorf("output = %q, want it to report the default 200-run cap when historyRetentionRuns is 0", out)
	}
	if !strings.Contains(out, "run-count cap, not a wall-clock one") {
		t.Error("output missing the honest run-count-vs-wall-clock caveat")
	}
}

// TestRenderEUAIActUsesCustomRetentionValue proves a non-zero override
// actually reaches the rendered report instead of always showing the
// package default.
func TestRenderEUAIActUsesCustomRetentionValue(t *testing.T) {
	out := RenderEUAIAct(Evidence{}, "path/to/skill", 400)
	if !strings.Contains(out, "400 run(s)") {
		t.Errorf("output = %q, want it to report the custom 400-run cap", out)
	}
	if strings.Contains(out, "the most recent 200 run(s)") {
		t.Error("output still shows the default 200 despite a custom override being passed")
	}
}

// TestRenderEUAIActSurfacesRedteamEvidence covers the same evidence
// RenderNISTAIRMF already surfaces under MEASURE 2.7 — the EU report was
// shipped without it despite Evidence computing it generically, so this
// is the gap-closing test.
func TestRenderEUAIActSurfacesRedteamEvidence(t *testing.T) {
	ev := Evidence{CasesWithRedteam: 2, RedteamPlugins: []string{"crescendo-jailbreak", "prompt-injection-canary"}}
	out := RenderEUAIAct(ev, "path/to/skill", 0)

	if !strings.Contains(out, "Article 15") {
		t.Error("output missing an Article 15 (Accuracy, Robustness and Cybersecurity) heading")
	}
	if !strings.Contains(out, "crescendo-jailbreak") || !strings.Contains(out, "prompt-injection-canary") {
		t.Errorf("output = %q, want it to name the redteam plugins in use", out)
	}
	if !strings.Contains(out, "2 case(s)") {
		t.Errorf("output = %q, want it to report the redteam case count", out)
	}
}

// TestRenderEUAIActNoRedteamPointsToPlugins covers the empty-evidence
// case: no redteam: assertions configured should still render a
// self-explanatory line, not a silently missing section.
func TestRenderEUAIActNoRedteamPointsToPlugins(t *testing.T) {
	out := RenderEUAIAct(Evidence{}, "path/to/skill", 0)
	if !strings.Contains(out, "Article 15") {
		t.Error("output missing an Article 15 heading even with no redteam evidence")
	}
	if !strings.Contains(out, "No `redteam:` assertions configured yet") {
		t.Errorf("output = %q, want an honest no-evidence-yet line", out)
	}
}
