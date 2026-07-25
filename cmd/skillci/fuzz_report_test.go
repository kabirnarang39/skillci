package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/fuzz"
	"github.com/kabirnarang39/skillci/internal/runner"
)

func TestPrintFuzzFindingsNoOutputWhenNoneFlipped(t *testing.T) {
	var buf bytes.Buffer
	printFuzzFindings(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("printFuzzFindings(nil) wrote %q, want no output", buf.String())
	}

	buf.Reset()
	printFuzzFindings(&buf, []fuzz.Finding{{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "x"}, Flipped: false}})
	if buf.Len() != 0 {
		t.Errorf("printFuzzFindings(none flipped) wrote %q, want no output", buf.String())
	}
}

func TestPrintFuzzFindingsShowsFlippedMutations(t *testing.T) {
	var buf bytes.Buffer
	printFuzzFindings(&buf, []fuzz.Finding{
		{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "unchanged"}, Triggered: true, Flipped: false},
		{Mutation: fuzz.Mutation{Operator: "synonym-swap", Prompt: "flipped one"}, Triggered: false, Flipped: true},
	})
	out := buf.String()
	if !strings.Contains(out, "[FUZZ]") {
		t.Errorf("output = %q, want a [FUZZ] line", out)
	}
	if !strings.Contains(out, "1/2") {
		t.Errorf("output = %q, want a 1/2 flipped count", out)
	}
	if !strings.Contains(out, "flipped one") {
		t.Errorf("output = %q, want the flipped mutation's prompt listed", out)
	}
	if strings.Contains(out, "unchanged") {
		t.Errorf("output = %q, want only the FLIPPED mutation listed, not the unflipped one", out)
	}
}

func TestPrintLatencyWarningNoOutputWhenNotExceeded(t *testing.T) {
	var buf bytes.Buffer
	printLatencyWarning(&buf, runner.Result{LatencyExceeded: false, LatencyMs: 50}, 5000)
	if buf.Len() != 0 {
		t.Errorf("printLatencyWarning(not exceeded) wrote %q, want no output", buf.String())
	}
}

func TestPrintLatencyWarningShowsWhenExceeded(t *testing.T) {
	var buf bytes.Buffer
	printLatencyWarning(&buf, runner.Result{LatencyExceeded: true, LatencyMs: 8000}, 5000)
	out := buf.String()
	if !strings.Contains(out, "[LATENCY]") {
		t.Errorf("output = %q, want a [LATENCY] line", out)
	}
	if !strings.Contains(out, "8000ms") || !strings.Contains(out, "5000ms") {
		t.Errorf("output = %q, want both the actual latency and the configured cap", out)
	}
}
