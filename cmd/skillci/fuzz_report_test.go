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

func TestPrintFuzzGeneratedCapNoticeWhenCapped(t *testing.T) {
	var buf bytes.Buffer
	findings := []fuzz.Finding{
		{Mutation: fuzz.Mutation{Operator: "typo-perturbation", Prompt: "a"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "case-mutation", Prompt: "b"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "whitespace-obfuscation", Prompt: "c"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "unicode-homoglyph", Prompt: "d"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "e"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "f"}, Flipped: true},
	}
	printFuzzGeneratedCapNotice(&buf, findings, true)
	if !strings.Contains(buf.String(), "5/6") {
		t.Errorf("output = %q, want it to state 5/6 flipped mutations were proposed as generated cases", buf.String())
	}
}

func TestPrintFuzzGeneratedCapNoticeNotCapped(t *testing.T) {
	var buf bytes.Buffer
	findings := []fuzz.Finding{
		{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "e"}, Flipped: true},
	}
	printFuzzGeneratedCapNotice(&buf, findings, true)
	if buf.String() != "" {
		t.Errorf("output = %q, want empty — only 1 flip, well under the cap", buf.String())
	}
}

func TestPrintFuzzGeneratedCapNoticeOnlyWhenGenerating(t *testing.T) {
	var buf bytes.Buffer
	findings := []fuzz.Finding{
		{Mutation: fuzz.Mutation{Operator: "typo-perturbation", Prompt: "a"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "case-mutation", Prompt: "b"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "whitespace-obfuscation", Prompt: "c"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "unicode-homoglyph", Prompt: "d"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "e"}, Flipped: true},
		{Mutation: fuzz.Mutation{Operator: "negation", Prompt: "f"}, Flipped: true},
	}
	// false means RunMatrix's self-growing fuzz path never fired for this
	// case+model (either it wasn't the first run, or the case isn't
	// fuzz_strict), so no GeneratedCases were proposed at all -- the notice
	// must stay silent, not claim a cap that was never actually applied.
	printFuzzGeneratedCapNotice(&buf, findings, false)
	if buf.String() != "" {
		t.Errorf("output = %q, want empty — this wasn't the case's first run, nothing was actually proposed", buf.String())
	}
}

func TestPrintFuzzCoverageZeroMutationsPrintsLine(t *testing.T) {
	var buf bytes.Buffer
	coverage := map[string]fuzz.OperatorCoverage{
		"typo-perturbation":      {Eligible: 0, Generated: 0},
		"case-mutation":          {Eligible: 0, Generated: 0},
		"whitespace-obfuscation": {Eligible: 0, Generated: 0},
		"unicode-homoglyph":      {Eligible: 0, Generated: 0},
	}
	printFuzzCoverage(&buf, nil, coverage)
	if !strings.Contains(buf.String(), "FUZZ COVERAGE") {
		t.Errorf("output = %q, want a [FUZZ COVERAGE] line when zero mutations were generated across all operators and there are no findings", buf.String())
	}
}

func TestPrintFuzzCoverageRealCoverageNoLine(t *testing.T) {
	var buf bytes.Buffer
	findings := []fuzz.Finding{{Mutation: fuzz.Mutation{Operator: "synonym-swap", Prompt: "x"}}}
	coverage := map[string]fuzz.OperatorCoverage{
		"typo-perturbation": {Eligible: 2, Generated: 2},
	}
	printFuzzCoverage(&buf, findings, coverage)
	if strings.Contains(buf.String(), "FUZZ COVERAGE") {
		t.Errorf("output = %q, want no coverage line — real mutations were generated (both from findings and from the coverage map)", buf.String())
	}
}

// TestPrintFuzzCoverageNilCoverageNoLine covers every case that never fuzzed
// at all: runner leaves Result.FuzzCoverage nil unless the fuzz block ran, and
// `skillci eval`/`regress` call this for EVERY case, not just fuzz-enabled
// ones. A nil map must stay silent — otherwise every ordinary eval case prints
// "fuzz testing ran but produced no real signal" about fuzzing that never ran.
func TestPrintFuzzCoverageNilCoverageNoLine(t *testing.T) {
	var buf bytes.Buffer
	printFuzzCoverage(&buf, nil, nil)
	if buf.Len() != 0 {
		t.Errorf("output = %q, want empty — this case never ran fuzzing, so there is no coverage claim to make", buf.String())
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
