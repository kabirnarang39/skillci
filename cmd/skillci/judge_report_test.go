package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/runner"
)

func TestPrintJudgeFindingsNoOutputWhenNilOrAllPass(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("printJudgeFindings(nil) wrote %q, want no output", buf.String())
	}

	buf.Reset()
	printJudgeFindings(&buf, []runner.JudgeFinding{{Name: "tone", Passed: true}})
	if buf.Len() != 0 {
		t.Errorf("printJudgeFindings(all pass) wrote %q, want no output", buf.String())
	}
}

func TestPrintJudgeFindingsShowsFailingCriteria(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, []runner.JudgeFinding{
		{Name: "tone", Passed: true},
		{Name: "empathy", Passed: false, Reason: "reads as curt"},
	})
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
	})
	out := buf.String()
	if !strings.Contains(out, "1/3 samples passed") {
		t.Errorf("output = %q, want it to contain the sample tally \"1/3 samples passed\"", out)
	}
}

func TestPrintJudgeFindingsOmitsSampleTallyWhenNotVoted(t *testing.T) {
	var buf bytes.Buffer
	printJudgeFindings(&buf, []runner.JudgeFinding{
		{Name: "tone", Passed: false, Reason: "too curt"},
	})
	out := buf.String()
	if strings.Contains(out, "samples passed") {
		t.Errorf("output = %q, want no sample tally when SampleCount is unset (single-sample case)", out)
	}
}
