package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/runner"
)

func TestPrintRedteamFindingsNoOutputWhenAllDefended(t *testing.T) {
	var buf bytes.Buffer
	printRedteamFindings(&buf, []runner.JudgeFinding{
		{Name: "prompt-injection-canary", Passed: true},
		{Name: "jailbreak-direct-override", Passed: true},
	})
	if buf.Len() != 0 {
		t.Errorf("output = %q, want empty — no attack succeeded", buf.String())
	}
}

func TestPrintRedteamFindingsReportsSuccessfulAttack(t *testing.T) {
	var buf bytes.Buffer
	printRedteamFindings(&buf, []runner.JudgeFinding{
		{Name: "prompt-injection-canary", Passed: false, Reason: "canary token CANARY-abc123ef leaked into the response"},
		{Name: "jailbreak-direct-override", Passed: true},
	})
	out := buf.String()
	if !strings.Contains(out, "1/2 attacks succeeded") {
		t.Errorf("output = %q, want a 1/2 attacks succeeded summary line", out)
	}
	if !strings.Contains(out, "prompt-injection-canary: SUCCEEDED") {
		t.Errorf("output = %q, want the failing plugin named as SUCCEEDED", out)
	}
	if strings.Contains(out, "jailbreak-direct-override: SUCCEEDED") {
		t.Errorf("output = %q, want the defended plugin NOT listed", out)
	}
}
