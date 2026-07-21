package badge

import (
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/history"
)

func TestStateFromRunAllPassing(t *testing.T) {
	run := history.Run{Cases: []history.CaseResult{{Passed: true}, {Passed: true}}}
	if got := StateFromRun(run); got != Passing {
		t.Errorf("StateFromRun() = %v, want Passing", got)
	}
}

func TestStateFromRunAllFailing(t *testing.T) {
	run := history.Run{Cases: []history.CaseResult{{Passed: false}, {Passed: false}}}
	if got := StateFromRun(run); got != Regressed {
		t.Errorf("StateFromRun() = %v, want Regressed", got)
	}
}

func TestStateFromRunMixed(t *testing.T) {
	run := history.Run{Cases: []history.CaseResult{{Passed: true}, {Passed: false}}}
	if got := StateFromRun(run); got != Partial {
		t.Errorf("StateFromRun() = %v, want Partial", got)
	}
}

func TestStateFromRunEmpty(t *testing.T) {
	if got := StateFromRun(history.Run{}); got != Regressed {
		t.Errorf("StateFromRun(empty) = %v, want Regressed (no passing evidence)", got)
	}
}

func TestRenderContainsStateAndIsValidSVG(t *testing.T) {
	svg := Render(Passing)
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Errorf("Render() = %q, not valid-looking SVG", svg)
	}
	if !strings.Contains(svg, "passing") {
		t.Errorf("Render(Passing) = %q, want it to contain the state label", svg)
	}
}
