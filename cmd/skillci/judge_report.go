package main

import (
	"fmt"
	"io"

	"github.com/kabirnarang39/skillci/internal/runner"
)

// printJudgeFindings prints a [JUDGE] line only when at least one
// criterion failed (non-verbose) — mirroring how the snapshot report line
// only prints on a detected Changed diff, not on every run. Under
// --verbose, every criterion that had real multi-sample voting
// (SampleCount > 1) is also shown individually — including ones that
// PASSED — because a split vote (not unanimous) is itself a reliability
// signal distinct from the verdict: 2026 LLM-judge-reliability research
// found judges disagree with themselves at near-coin-flip rates on close
// calls, so a 2/3 pass and a 3/3 pass carry very different confidence
// even though both "pass."
func printJudgeFindings(w io.Writer, findings []runner.JudgeFinding, sampleDetail map[string][]runner.JudgeFinding, verbose bool) {
	failed := 0
	for _, f := range findings {
		if !f.Passed {
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(w, "  [JUDGE] %d/%d criteria failed\n", failed, len(findings))
		for _, f := range findings {
			if f.Passed {
				continue
			}
			if f.SampleCount > 1 {
				fmt.Fprintf(w, "    %s: FAIL (%d/%d samples passed) — %s\n", f.Name, f.PassCount, f.SampleCount, f.Reason)
			} else {
				fmt.Fprintf(w, "    %s: FAIL — %s\n", f.Name, f.Reason)
			}
		}
	}

	if !verbose {
		return
	}
	for _, f := range findings {
		if f.SampleCount <= 1 {
			continue
		}
		// Every voted criterion gets its own header line here, passing or
		// failing: the per-sample lines below carry no criterion name, so
		// without a header per criterion two voted criteria's breakdowns
		// run together indistinguishably. A failing criterion's summary
		// line was already printed above, but that one is non-verbose
		// output that must stay byte-identical (no marker), so the
		// split-vote marker only ever appears on this verbose header.
		verdict := "PASS"
		if !f.Passed {
			verdict = "FAIL"
		}
		splitMarker := ""
		if f.PassCount != 0 && f.PassCount != f.SampleCount {
			splitMarker = " ⚠ SPLIT VOTE — not unanimous, treat with reduced confidence"
		}
		fmt.Fprintf(w, "  [JUDGE] %s: %s (%d/%d samples)%s\n", f.Name, verdict, f.PassCount, f.SampleCount, splitMarker)
		for i, sample := range sampleDetail[f.Name] {
			if sample.Passed {
				fmt.Fprintf(w, "    sample %d: PASS\n", i+1)
			} else {
				fmt.Fprintf(w, "    sample %d: FAIL — %s\n", i+1, sample.Reason)
			}
		}
	}
}
