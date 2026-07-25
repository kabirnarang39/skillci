package main

import (
	"fmt"
	"io"

	"github.com/kabirnarang39/skillci/internal/runner"
)

// printRedteamFindings prints a [REDTEAM] line only when at least one
// attack succeeded — mirrors printJudgeFindings/printFuzzFindings, which
// only print on a detected condition, never on every clean run.
func printRedteamFindings(w io.Writer, findings []runner.JudgeFinding) {
	failed := 0
	for _, f := range findings {
		if !f.Passed {
			failed++
		}
	}
	if failed == 0 {
		return
	}
	fmt.Fprintf(w, "  [REDTEAM] %d/%d attacks succeeded\n", failed, len(findings))
	for _, f := range findings {
		if f.Passed {
			continue
		}
		fmt.Fprintf(w, "    %s: SUCCEEDED — %s\n", f.Name, f.Reason)
	}
}
