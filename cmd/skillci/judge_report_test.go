package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/runner"
)

func TestPrintJudgeFindingsNoOutputWhenNilOrAllPass(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, nil, nil, false)
	if buf.Len() != 0 {
		t.Errorf("printJudgeFindings(nil) wrote %q, want no output", buf.String())
	}

	buf.Reset()
	printJudgeFindings(&buf, []runner.JudgeFinding{{Name: "tone", Passed: true}}, nil, false)
	if buf.Len() != 0 {
		t.Errorf("printJudgeFindings(all pass) wrote %q, want no output", buf.String())
	}
}

func TestPrintJudgeFindingsShowsFailingCriteria(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, []runner.JudgeFinding{
		{Name: "tone", Passed: true},
		{Name: "empathy", Passed: false, Reason: "reads as curt"},
	}, nil, false)
	out := buf.String()
	if !strings.Contains(out, "[JUDGE]") {
		t.Errorf("output = %q, want a [JUDGE] line", out)
	}
	if !strings.Contains(out, "1/2") {
		t.Errorf("output = %q, want a 1/2 failed count", out)
	}
	if !strings.Contains(out, "empathy") || !strings.Contains(out, "reads as curt") {
		t.Errorf("output = %q, want the failing criterion name and reason", out)
	}
	if strings.Contains(out, "tone") {
		t.Errorf("output = %q, want only the FAILING criterion listed, not the passing one", out)
	}
}

func TestPrintJudgeFindingsShowsSampleTallyWhenVoted(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, []runner.JudgeFinding{
		{Name: "tone", Passed: false, Reason: "too curt", SampleCount: 3, PassCount: 1},
	}, nil, false)
	out := buf.String()
	if !strings.Contains(out, "1/3 samples passed") {
		t.Errorf("output = %q, want it to contain the sample tally \"1/3 samples passed\"", out)
	}
}

func TestPrintJudgeFindingsOmitsSampleTallyWhenNotVoted(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, []runner.JudgeFinding{
		{Name: "tone", Passed: false, Reason: "too curt"},
	}, nil, false)
	out := buf.String()
	if strings.Contains(out, "samples passed") {
		t.Errorf("output = %q, want no sample tally when SampleCount is unset (single-sample case)", out)
	}
}

func TestPrintJudgeFindingsVerboseShowsPerSampleBreakdown(t *testing.T) {
	var buf bytes.Buffer
	findings := []runner.JudgeFinding{
		{Name: "tone", Passed: true, SampleCount: 3, PassCount: 2},
	}
	detail := map[string][]runner.JudgeFinding{
		"tone": {
			{Name: "tone", Passed: true},
			{Name: "tone", Passed: false, Reason: "too curt"},
			{Name: "tone", Passed: true},
		},
	}
	printJudgeFindings(&buf, findings, detail, true)
	out := buf.String()
	if !strings.Contains(out, "sample 1: PASS") {
		t.Errorf("output = %q, want a per-sample line for sample 1", out)
	}
	if !strings.Contains(out, "sample 2: FAIL") || !strings.Contains(out, "too curt") {
		t.Errorf("output = %q, want a per-sample line for sample 2 with its reason", out)
	}
	if !strings.Contains(out, "sample 3: PASS") {
		t.Errorf("output = %q, want a per-sample line for sample 3", out)
	}
}

func TestPrintJudgeFindingsVerboseFlagsSplitVote(t *testing.T) {
	var buf bytes.Buffer
	findings := []runner.JudgeFinding{
		{Name: "tone", Passed: true, SampleCount: 3, PassCount: 2},
	}
	detail := map[string][]runner.JudgeFinding{
		"tone": {{Passed: true}, {Passed: false}, {Passed: true}},
	}
	printJudgeFindings(&buf, findings, detail, true)
	if !strings.Contains(buf.String(), "SPLIT VOTE") {
		t.Errorf("output = %q, want a SPLIT VOTE marker for a 2/3 non-unanimous vote", buf.String())
	}
}

func TestPrintJudgeFindingsVerboseNoSplitVoteMarkerWhenUnanimous(t *testing.T) {
	var buf bytes.Buffer
	findings := []runner.JudgeFinding{
		{Name: "tone", Passed: true, SampleCount: 3, PassCount: 3},
	}
	detail := map[string][]runner.JudgeFinding{
		"tone": {{Passed: true}, {Passed: true}, {Passed: true}},
	}
	printJudgeFindings(&buf, findings, detail, true)
	if strings.Contains(buf.String(), "SPLIT VOTE") {
		t.Errorf("output = %q, want no SPLIT VOTE marker for a unanimous 3/3 vote", buf.String())
	}
}

func TestPrintJudgeFindingsVerboseShowsPassingVotedCriteria(t *testing.T) {
	// A criterion that PASSED overall but by a split vote must still be
	// visible under --verbose (the instability is the signal, not the
	// verdict) — non-verbose mode already hides it (per
	// TestPrintJudgeFindingsNoOutputWhenNilOrAllPass), so this only
	// needs to prove verbose mode surfaces it.
	var buf bytes.Buffer
	findings := []runner.JudgeFinding{
		{Name: "tone", Passed: true, SampleCount: 3, PassCount: 2},
	}
	detail := map[string][]runner.JudgeFinding{
		"tone": {{Passed: true}, {Passed: false}, {Passed: true}},
	}
	printJudgeFindings(&buf, findings, detail, true)
	if !strings.Contains(buf.String(), "tone") {
		t.Errorf("output = %q, want the passing-but-split criterion \"tone\" to be shown under --verbose", buf.String())
	}
}

func TestPrintJudgeFindingsNonVerboseUnchangedByNewParams(t *testing.T) {
	var buf bytes.Buffer
	findings := []runner.JudgeFinding{
		{Name: "tone", Passed: true},
		{Name: "empathy", Passed: false, Reason: "reads as curt"},
	}
	detail := map[string][]runner.JudgeFinding{"tone": {{Passed: true}}}
	printJudgeFindings(&buf, findings, detail, false)
	out := buf.String()
	if strings.Contains(out, "sample 1") || strings.Contains(out, "SPLIT VOTE") {
		t.Errorf("output = %q, want non-verbose output unaffected by a populated sampleDetail map", out)
	}
	if !strings.Contains(out, "[JUDGE] 1/2 criteria failed") {
		t.Errorf("output = %q, want the existing non-verbose summary line unchanged", out)
	}
}
