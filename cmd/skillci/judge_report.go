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
				splitMarker := ""
				if f.PassCount != 0 && f.PassCount != f.SampleCount {
					splitMarker = " ⚠ SPLIT VOTE"
				}
				fmt.Fprintf(w, "    %s: FAIL (%d/%d samples passed)%s — %s\n", f.Name, f.PassCount, f.SampleCount, splitMarker, f.Reason)
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
		if !f.Passed {
			// Already printed above under the FAIL summary — verbose mode
			// still adds the per-sample breakdown for it below, so it
			// isn't skipped, just not re-printed as a standalone header.
		} else {
			splitMarker := ""
			if f.PassCount != 0 && f.PassCount != f.SampleCount {
				splitMarker = " ⚠ SPLIT VOTE — not unanimous, treat with reduced confidence"
			}
			fmt.Fprintf(w, "  [JUDGE] %s: PASS (%d/%d samples)%s\n", f.Name, f.PassCount, f.SampleCount, splitMarker)
		}
		for i, sample := range sampleDetail[f.Name] {
			if sample.Passed {
				fmt.Fprintf(w, "    sample %d: PASS\n", i+1)
			} else {
				fmt.Fprintf(w, "    sample %d: FAIL — %s\n", i+1, sample.Reason)
			}
		}
	}
}
