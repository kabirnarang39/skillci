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

// printFuzzCoverage prints a [FUZZ COVERAGE] line only when zero mutations
// exist across both findings and the coverage map's Generated counts — a
// prompt that dodged every operator entirely (no synonym hits, every word
// too short for the length-gated operators) gets a visible "nothing was
// actually tested" note instead of a silent, indistinguishable-from-covered
// pass. Never printed when real mutations exist, even if some individual
// operator's coverage is zero — that's normal (not every operator applies
// to every prompt).
func printFuzzCoverage(w io.Writer, findings []fuzz.Finding, coverage map[string]fuzz.OperatorCoverage) {
	if len(findings) > 0 {
		return
	}
	totalGenerated := 0
	for _, c := range coverage {
		totalGenerated += c.Generated
	}
	if totalGenerated > 0 {
		return
	}
	fmt.Fprintln(w, "  [FUZZ COVERAGE] 0 mutations generated across all operators — this prompt had no applicable mutation (too short, or no synonym-map hits); fuzz testing ran but produced no real signal")
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
