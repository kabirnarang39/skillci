package main

import (
	"fmt"
	"io"

	"github.com/kabirnarang39/skillci/internal/fuzz"
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
