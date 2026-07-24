package main

import (
	"fmt"
	"io"

	"github.com/kabirnarang39/skillci/internal/fuzz"
	"github.com/kabirnarang39/skillci/internal/runner"
)

// printFuzzFindings prints a [FUZZ] summary only when at least one mutation
// flipped the case's expected trigger outcome — mirroring how the snapshot
// report line only prints on a detected Changed diff, not on every run.
func printFuzzFindings(w io.Writer, findings []fuzz.Finding) {
	flipped := 0
	for _, f := range findings {
		if f.Flipped {
			flipped++
		}
	}
	if flipped == 0 {
		return
	}
	fmt.Fprintf(w, "  [FUZZ] %d/%d mutations flipped trigger behavior\n", flipped, len(findings))
	for _, f := range findings {
		if f.Flipped {
			fmt.Fprintf(w, "    %s: %q -> triggered=%v (want %v)\n", f.Mutation.Operator, f.Mutation.Prompt, f.Triggered, !f.Triggered)
		}
	}
}

// printLatencyWarning prints a [LATENCY] line only when result.LatencyExceeded
// is true — mirroring how [SNAPSHOT CHANGED]/[FUZZ] only print on a
// detected condition, never on every passing run. maxLatencyMs is the
// case's configured cap, purely for the message text.
func printLatencyWarning(w io.Writer, result runner.Result, maxLatencyMs int64) {
	if !result.LatencyExceeded {
		return
	}
	fmt.Fprintf(w, "  [LATENCY] %dms exceeds max_latency_ms cap of %dms (informational — set latency_strict: true to fail CI on this)\n", result.LatencyMs, maxLatencyMs)
}
