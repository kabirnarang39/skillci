package main

import (
	"fmt"
	"io"

	"github.com/kabirnarang39/skillci/internal/runner"
)

// printJudgeFindings prints a [JUDGE] line only when at least one
// criterion failed — mirroring how [FUZZ]/[LATENCY]/[RETRY] only print on
// a detected condition, never on every passing run. Only the failing
// criteria are listed individually.
func printJudgeFindings(w io.Writer, findings []runner.JudgeFinding) {
	failed := 0
	for _, f := range findings {
		if !f.Passed {
			failed++
		}
	}
	if failed == 0 {
		return
	}
	fmt.Fprintf(w, "  [JUDGE] %d/%d criteria failed\n", failed, len(findings))
	for _, f := range findings {
		if f.Passed {
			continue
		}
		fmt.Fprintf(w, "    %s: FAIL — %s\n", f.Name, f.Reason)
	}
}
