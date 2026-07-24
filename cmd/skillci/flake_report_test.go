package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/runner"
)

func TestPrintFlakeReportNoOutputWhenVerdictEmpty(t *testing.T) {
	var buf bytes.Buffer
	printFlakeReport(&buf, runner.Result{FlakeVerdict: ""}, false)
	if buf.Len() != 0 {
		t.Errorf("printFlakeReport() wrote %q, want no output when FlakeVerdict is empty", buf.String())
	}
}

func TestPrintFlakeReportConfirmedFail(t *testing.T) {
	var buf bytes.Buffer
	printFlakeReport(&buf, runner.Result{FlakeVerdict: "confirmed_fail", FlakeAttemptsPassed: 0, FlakeAttemptsTotal: 3}, false)
	out := buf.String()
	if !strings.Contains(out, "[RETRY]") || !strings.Contains(out, "confirmed failing") || !strings.Contains(out, "0/3") {
		t.Errorf("output = %q, want a [RETRY] line reporting confirmed failing 0/3", out)
	}
}

func TestPrintFlakeReportConfirmedPass(t *testing.T) {
	var buf bytes.Buffer
	printFlakeReport(&buf, runner.Result{FlakeVerdict: "confirmed_pass", FlakeAttemptsPassed: 2, FlakeAttemptsTotal: 3}, false)
	out := buf.String()
	if !strings.Contains(out, "[RETRY]") || !strings.Contains(out, "confirmed passing") || !strings.Contains(out, "2/3") {
		t.Errorf("output = %q, want a [RETRY] line reporting confirmed passing 2/3", out)
	}
}

func TestPrintFlakeReportUnstableNonStrictMentionsInformational(t *testing.T) {
	var buf bytes.Buffer
	printFlakeReport(&buf, runner.Result{FlakeVerdict: "unstable", FlakeAttemptsPassed: 1, FlakeAttemptsTotal: 2}, false)
	out := buf.String()
	if !strings.Contains(out, "unstable") || !strings.Contains(out, "1/2") || !strings.Contains(out, "flake_strict") {
		t.Errorf("output = %q, want an unstable line mentioning 1/2 and flake_strict", out)
	}
}

func TestPrintFlakeReportUnstableStrictMentionsFailed(t *testing.T) {
	var buf bytes.Buffer
	printFlakeReport(&buf, runner.Result{FlakeVerdict: "unstable", FlakeAttemptsPassed: 1, FlakeAttemptsTotal: 2}, true)
	out := buf.String()
	if !strings.Contains(out, "unstable") || !strings.Contains(out, "1/2") {
		t.Errorf("output = %q, want an unstable line mentioning 1/2", out)
	}
}
