package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/regress"
	"github.com/kabirnarang39/skillci/internal/runner"
)

func TestPrintDimensionRollupNoOutputWhenNoDimensions(t *testing.T) {
	outcomes := []regress.Outcome{
		{Case: evalspec.Case{Name: "plain-case"}, Model: "claude-sonnet-5", Result: runner.Result{Passed: true}},
	}
	var buf bytes.Buffer
	printDimensionRollup(&buf, outcomes)
	if buf.Len() != 0 {
		t.Errorf("printDimensionRollup() wrote %q, want no output when no case has dimensions", buf.String())
	}
}

func TestPrintDimensionRollupGroupsByKeyAndValue(t *testing.T) {
	outcomes := []regress.Outcome{
		{
			Case:   evalspec.Case{Name: "ent-1", Dimensions: map[string]string{"segment": "enterprise"}},
			Model:  "claude-sonnet-5",
			Result: runner.Result{Passed: true},
		},
		{
			Case:   evalspec.Case{Name: "ent-2", Dimensions: map[string]string{"segment": "enterprise"}},
			Model:  "claude-sonnet-5",
			Result: runner.Result{Passed: false},
		},
		{
			Case:   evalspec.Case{Name: "free-1", Dimensions: map[string]string{"segment": "free"}},
			Model:  "claude-sonnet-5",
			Result: runner.Result{Passed: true},
		},
	}
	var buf bytes.Buffer
	printDimensionRollup(&buf, outcomes)
	out := buf.String()
	if !strings.Contains(out, "--- by dimension ---") {
		t.Errorf("output = %q, want a --- by dimension --- header", out)
	}
	if !strings.Contains(out, "segment=enterprise: 1/2 passed") {
		t.Errorf("output = %q, want segment=enterprise: 1/2 passed", out)
	}
	if !strings.Contains(out, "ent-2") {
		t.Errorf("output = %q, want the failing case ent-2 listed under segment=enterprise", out)
	}
	if !strings.Contains(out, "segment=free: 1/1 passed") {
		t.Errorf("output = %q, want segment=free: 1/1 passed", out)
	}
	if strings.Contains(out, "free-1") {
		t.Errorf("output = %q, a fully-passing group must not list its passing cases individually", out)
	}
}

func TestPrintDimensionRollupCaseAppearsInEveryGroupItBelongsTo(t *testing.T) {
	outcomes := []regress.Outcome{
		{
			Case:   evalspec.Case{Name: "multi-dim", Dimensions: map[string]string{"segment": "enterprise", "language": "es"}},
			Model:  "claude-sonnet-5",
			Result: runner.Result{Passed: false},
		},
	}
	var buf bytes.Buffer
	printDimensionRollup(&buf, outcomes)
	out := buf.String()
	if !strings.Contains(out, "segment=enterprise: 0/1 passed") {
		t.Errorf("output = %q, want segment=enterprise: 0/1 passed", out)
	}
	if !strings.Contains(out, "language=es: 0/1 passed") {
		t.Errorf("output = %q, want language=es: 0/1 passed", out)
	}
}

func TestPrintDimensionRollupMarksStrictDimensionFail(t *testing.T) {
	outcomes := []regress.Outcome{
		{
			Case:                evalspec.Case{Name: "strict-fail-case", Dimensions: map[string]string{"segment": "enterprise"}},
			Model:               "claude-sonnet-5",
			Result:              runner.Result{Passed: false},
			StrictDimensionFail: true,
		},
	}
	var buf bytes.Buffer
	printDimensionRollup(&buf, outcomes)
	out := buf.String()
	if !strings.Contains(out, "[STRICT]") {
		t.Errorf("output = %q, want a [STRICT] marker for a StrictDimensionFail outcome", out)
	}
}
