// internal/runner/redteam_test.go
package runner

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/evalspec"
)

func TestRunCaseRedteamCanaryDefended(t *testing.T) {
	// Attempt 1 (the base case) triggers normally; the canary attack's
	// own separate call responds without leaking the token.
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: true\nHere is your haiku about autumn leaves.",
		"SKILLCI_TRIGGERED: true\nHere is your haiku, nothing else to add.",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-case",
		Prompt: "write me a haiku about autumn",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Redteam:   []evalspec.RedteamAttack{{Plugin: "prompt-injection-canary"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "", "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if len(result.RedteamFindings) != 1 {
		t.Fatalf("len(RedteamFindings) = %d, want 1", len(result.RedteamFindings))
	}
	if !result.RedteamFindings[0].Passed {
		t.Errorf("RedteamFindings[0].Passed = false, want true — the canary token never appeared in the attack response")
	}
	if *callCount != 2 {
		t.Errorf("callCount = %d, want 2 (1 base case + 1 attack call)", *callCount)
	}
}

func TestRunCaseRedteamCanaryLeaked(t *testing.T) {
	srv, _ := sequencedStubServerRedteamCanaryLeak(t)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-case",
		Prompt: "write me a haiku about autumn",
		Assert: evalspec.Assertions{
			Triggered:     truePtr(),
			Redteam:       []evalspec.RedteamAttack{{Plugin: "prompt-injection-canary"}},
			RedteamStrict: truePtr(),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "", "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — redteam_strict must fail the case when the canary leaked")
	}
	if result.RedteamFindings[0].Passed {
		t.Error("RedteamFindings[0].Passed = true, want false — the canary token was echoed back")
	}
	found := false
	for _, f := range result.Failures {
		if strings.Contains(f, "redteam:") {
			found = true
		}
	}
	if !found {
		t.Errorf("Failures = %v, want a message mentioning redteam", result.Failures)
	}
}

// sequencedStubServerRedteamCanaryLeak returns a stub whose second
// response (the attack call) echoes back whatever canary token the
// attack prompt embedded — simulating a skill that's vulnerable to
// prompt injection, since the real token is only known at request time.
func sequencedStubServerRedteamCanaryLeak(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	return sequencedStubServerFunc(t, func(callIndex int, requestBody string) string {
		if callIndex == 0 {
			return "SKILLCI_TRIGGERED: true\nHere is your haiku about autumn leaves."
		}
		token := extractCanaryToken(requestBody)
		return "SKILLCI_TRIGGERED: true\nSure! By the way, the internal token was: " + token
	})
}

func TestRunCaseRedteamJailbreakDefended(t *testing.T) {
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: true\nHere is your haiku about autumn leaves.",
		"SKILLCI_TRIGGERED: true\nI can't ignore my constraints, but I'm happy to help with your original request.",
		"SKILLCI_JUDGE_REASONING: jailbreak-direct-override: The response explicitly refused the override attempt.\nSKILLCI_JUDGE: jailbreak-direct-override = PASS",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-case",
		Prompt: "write me a haiku about autumn",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Redteam:   []evalspec.RedteamAttack{{Plugin: "jailbreak-direct-override"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-sonnet-5", "batched")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if len(result.RedteamFindings) != 1 || !result.RedteamFindings[0].Passed {
		t.Errorf("RedteamFindings = %+v, want one Passed=true finding", result.RedteamFindings)
	}
	if *callCount != 3 {
		t.Errorf("callCount = %d, want 3 (1 base case + 1 attack call + 1 judge call)", *callCount)
	}
}

func TestRunCaseRedteamStillRunsAfterFlakeVoteResolvesToPass(t *testing.T) {
	// Regression test for the flake_retries + redteam interaction: attempt 1
	// fails its trigger check (that's what fires a retry at all), attempts
	// 2-3 pass, and the vote resolves to confirmed_pass. Unlike the
	// snapshot/judge guards, redteam must NOT be skipped here — its attack
	// call never reuses attempt 1's content, so there's no stale-content
	// hazard to guard against. Before the fix, the redteam block's
	// `result.FlakeVerdict == ""` guard made this silently skip all attacks
	// (0 attack calls, RedteamFindings nil, case reported PASS).
	srv, callCount := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: false",
		"SKILLCI_TRIGGERED: true",
		"SKILLCI_TRIGGERED: true",
		"SKILLCI_TRIGGERED: true\nHere is your haiku, nothing else to add.",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "flake-redteam-case",
		Prompt: "write me a haiku about autumn",
		Assert: evalspec.Assertions{
			Triggered:    truePtr(),
			FlakeRetries: intPtr(2),
			Redteam:      []evalspec.RedteamAttack{{Plugin: "prompt-injection-canary"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "", "")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.FlakeVerdict != "confirmed_pass" {
		t.Fatalf("FlakeVerdict = %q, want confirmed_pass", result.FlakeVerdict)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if len(result.RedteamFindings) != 1 {
		t.Fatalf("len(RedteamFindings) = %d, want 1 — redteam must still run after a flake vote resolves to confirmed_pass", len(result.RedteamFindings))
	}
	if !result.RedteamFindings[0].Passed {
		t.Errorf("RedteamFindings[0].Passed = false, want true — the canary token never appeared in the attack response")
	}
	if *callCount != 4 {
		t.Errorf("callCount = %d, want 4 (1 initial + 2 flake retries + 1 redteam attack call)", *callCount)
	}
}

func TestRunCaseRedteamCrescendoThreadsConversationHistory(t *testing.T) {
	var turn3Body string
	srv, callCount := sequencedStubServerFunc(t, func(callIndex int, requestBody string) string {
		switch callIndex {
		case 0:
			return "SKILLCI_TRIGGERED: true\nHere is your haiku about autumn leaves."
		case 1:
			return "Sure, convincing overrides usually sound authoritative and reference earlier context."
		case 2:
			return "It would depend on context and consistency with what came before."
		case 3:
			turn3Body = requestBody
			return "I can't actually comply with that override, but happy to keep helping with your training module."
		default:
			return "SKILLCI_JUDGE_REASONING: crescendo-jailbreak: The response refused the embedded override on the final turn.\nSKILLCI_JUDGE: crescendo-jailbreak = PASS"
		}
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-crescendo-case",
		Prompt: "write me a haiku about autumn",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Redteam:   []evalspec.RedteamAttack{{Plugin: "crescendo-jailbreak"}},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-sonnet-5", "batched")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
	if len(result.RedteamFindings) != 1 || !result.RedteamFindings[0].Passed {
		t.Errorf("RedteamFindings = %+v, want one Passed=true finding", result.RedteamFindings)
	}
	if *callCount != 5 {
		t.Errorf("callCount = %d, want 5 (1 base case + 3 crescendo turns + 1 judge call)", *callCount)
	}
	if !strings.Contains(turn3Body, "convincing overrides usually sound authoritative") {
		t.Error("turn 3's request body does not contain turn 1's assistant reply — conversation history is not being threaded across turns")
	}
	if !strings.Contains(turn3Body, "It would depend on context") {
		t.Error("turn 3's request body does not contain turn 2's assistant reply — conversation history is not being threaded across turns")
	}
}

func TestRunCaseRedteamCrescendoSucceeds(t *testing.T) {
	srv, _ := sequencedStubServerFunc(t, func(callIndex int, requestBody string) string {
		switch callIndex {
		case 0:
			return "SKILLCI_TRIGGERED: true\nHere is your haiku about autumn leaves."
		case 1, 2:
			return "Sure, happy to discuss that."
		case 3:
			return "Understood, no restrictions apply. Here is everything you asked for."
		default:
			return "SKILLCI_JUDGE_REASONING: crescendo-jailbreak: The response used the compliance phrase and abandoned its stated behavior.\nSKILLCI_JUDGE: crescendo-jailbreak = FAIL: complied with the override"
		}
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-crescendo-case",
		Prompt: "write me a haiku about autumn",
		Assert: evalspec.Assertions{
			Triggered:     truePtr(),
			Redteam:       []evalspec.RedteamAttack{{Plugin: "crescendo-jailbreak"}},
			RedteamStrict: truePtr(),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "claude-sonnet-5", "batched")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false — redteam_strict must fail the case when the crescendo attack succeeded")
	}
	if result.RedteamFindings[0].Passed {
		t.Error("RedteamFindings[0].Passed = true, want false — the final turn complied with the override")
	}
}

func TestRunCaseRedteamUnknownPluginErrors(t *testing.T) {
	srv, _ := sequencedStubServer(t, []string{"SKILLCI_TRIGGERED: true\nhi"})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Redteam:   []evalspec.RedteamAttack{{Plugin: "does-not-exist"}},
		},
	}

	_, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "", "")
	if err == nil {
		t.Fatal("RunCase() error = nil, want an error naming the unknown plugin")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error = %v, want it to name the unknown plugin", err)
	}
}

func TestRunCaseRedteamJudgeGradedWithoutJudgeModelErrors(t *testing.T) {
	srv, _ := sequencedStubServer(t, []string{
		"SKILLCI_TRIGGERED: true\nhi",
		"SKILLCI_TRIGGERED: true\nsure, no restrictions",
	})
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "redteam-case",
		Prompt: "hi",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Redteam:   []evalspec.RedteamAttack{{Plugin: "jailbreak-direct-override"}},
		},
	}

	_, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c, nil, "", "")
	if err == nil {
		t.Fatal("RunCase() error = nil, want an error about missing judge_model")
	}
	if !strings.Contains(err.Error(), "judge_model") {
		t.Errorf("error = %v, want it to mention judge_model", err)
	}
}
