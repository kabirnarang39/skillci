package main

import (
	"fmt"
	"io"

	"github.com/kabirnarang39/skillci/internal/runner"
)

// printFlakeReport prints a [RETRY] line only when result.FlakeVerdict is
// non-empty — mirroring how [SNAPSHOT CHANGED]/[FUZZ]/[LATENCY] only print
// on a detected condition, never on every passing run. flakeStrict is the
// case's configured flake_strict value, purely for the message text.
func printFlakeReport(w io.Writer, result runner.Result, flakeStrict bool) {
	switch result.FlakeVerdict {
	case "confirmed_pass":
		fmt.Fprintf(w, "  [RETRY] triggered check confirmed passing after initial flake — %d/%d attempts passed\n", result.FlakeAttemptsPassed, result.FlakeAttemptsTotal)
	case "confirmed_fail":
		fmt.Fprintf(w, "  [RETRY] triggered check confirmed failing — %d/%d attempts passed\n", result.FlakeAttemptsPassed, result.FlakeAttemptsTotal)
	case "unstable":
		if flakeStrict {
			fmt.Fprintf(w, "  [RETRY] triggered check unstable after %d attempts — %d/%d passed (tie), failing because flake_strict is set\n", result.FlakeAttemptsTotal, result.FlakeAttemptsPassed, result.FlakeAttemptsTotal)
		} else {
			fmt.Fprintf(w, "  [RETRY] triggered check unstable after %d attempts — %d/%d passed (tie), informational only unless flake_strict is set\n", result.FlakeAttemptsTotal, result.FlakeAttemptsPassed, result.FlakeAttemptsTotal)
		}
	}
}
