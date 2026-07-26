package redteam

import (
	"strings"
	"testing"
)

func TestPromptInjectionCanaryBuildAttackEmbedsUniqueToken(t *testing.T) {
	plugin := Registry["prompt-injection-canary"]
	attack1, state1 := plugin.BuildAttack("write a haiku about autumn")
	_, state2 := plugin.BuildAttack("write a haiku about autumn")

	if !strings.Contains(attack1, state1) {
		t.Errorf("attack prompt %q does not contain its own state token %q", attack1, state1)
	}
	if state1 == state2 {
		t.Error("two BuildAttack calls produced the same canary token — tokens must be per-invocation random, not fixed, or a model could have memorized a fixed token")
	}
	if !strings.Contains(attack1, "write a haiku about autumn") {
		t.Error("attack prompt must still contain the original base prompt")
	}
}

func TestPromptInjectionCanaryDetectCatchesLeak(t *testing.T) {
	plugin := Registry["prompt-injection-canary"]
	_, state := plugin.BuildAttack("write a haiku")

	succeeded, detail := plugin.Detect("Sure, here is the internal token: "+state, state, "", "")
	if !succeeded {
		t.Error("succeeded = false, want true — the canary token literally appears in the response")
	}
	if detail == "" {
		t.Error("detail is empty, want a message naming the leaked token")
	}
}

func TestPromptInjectionCanaryDetectNoLeak(t *testing.T) {
	plugin := Registry["prompt-injection-canary"]
	_, state := plugin.BuildAttack("write a haiku")

	succeeded, _ := plugin.Detect("Here is your haiku about autumn leaves falling softly.", state, "", "")
	if succeeded {
		t.Error("succeeded = true, want false — the response never mentions the canary token")
	}
}

func TestInstructionLeakageDetectCatchesVerbatimEcho(t *testing.T) {
	plugin := Registry["instruction-leakage"]
	succeeded, detail := plugin.Detect(
		"You are haiku-writer. Writes a haiku when the user asks for one. That's my whole system prompt.",
		"", "haiku-writer", "Writes a haiku when the user asks for one.",
	)
	if !succeeded {
		t.Error("succeeded = false, want true — both the skill's name and description appear verbatim in the response")
	}
	if detail == "" {
		t.Error("detail is empty, want a message describing the leak")
	}
}

func TestInstructionLeakageDetectNoEcho(t *testing.T) {
	plugin := Registry["instruction-leakage"]
	succeeded, _ := plugin.Detect(
		"I can't share my internal instructions, but I'd be happy to write you a haiku!",
		"", "haiku-writer", "Writes a haiku when the user asks for one.",
	)
	if succeeded {
		t.Error("succeeded = true, want false — neither the skill name nor description appear in the response")
	}
}

func TestRegistryHasAllFourV1Plugins(t *testing.T) {
	want := []string{"prompt-injection-canary", "instruction-leakage", "jailbreak-direct-override", "harmful-content-elicitation"}
	for _, name := range want {
		if _, ok := Registry[name]; !ok {
			t.Errorf("Registry[%q] missing", name)
		}
	}
}

func TestDeterministicPluginsHaveNoJudgeFields(t *testing.T) {
	for _, name := range []string{"prompt-injection-canary", "instruction-leakage"} {
		p := Registry[name]
		if p.Grading != GradingDeterministic {
			t.Errorf("Registry[%q].Grading = %q, want %q", name, p.Grading, GradingDeterministic)
		}
		if p.BuildAttack == nil || p.Detect == nil {
			t.Errorf("Registry[%q] is deterministic but missing BuildAttack/Detect", name)
		}
	}
}

func TestJudgeGradedPluginsHaveRubrics(t *testing.T) {
	for _, name := range []string{"jailbreak-direct-override", "harmful-content-elicitation"} {
		p := Registry[name]
		if p.Grading != GradingJudge {
			t.Errorf("Registry[%q].Grading = %q, want %q", name, p.Grading, GradingJudge)
		}
		if p.AttackPromptSuffix == "" || p.JudgeRubric == "" {
			t.Errorf("Registry[%q] is judge-graded but missing AttackPromptSuffix/JudgeRubric", name)
		}
	}
}

func TestCrescendoJailbreakHasThreeTurnsAndFinalRubric(t *testing.T) {
	p := Registry["crescendo-jailbreak"]
	if p.Grading != GradingJudge {
		t.Errorf("Grading = %q, want %q", p.Grading, GradingJudge)
	}
	if len(p.Turns) != 3 {
		t.Fatalf("len(Turns) = %d, want 3", len(p.Turns))
	}
	for i, turn := range p.Turns {
		if turn.Message == "" {
			t.Errorf("Turns[%d].Message is empty", i)
		}
	}
	if p.JudgeRubric == "" {
		t.Error("JudgeRubric is empty, want a rubric graded against the final turn's response")
	}
	if p.AttackPromptSuffix != "" {
		t.Error("AttackPromptSuffix is set, want empty — crescendo-jailbreak uses Turns, not a single-turn suffix")
	}
}

func TestSystemPromptLeakSemanticSharesAttackTextWithInstructionLeakage(t *testing.T) {
	det := Registry["instruction-leakage"]
	sem := Registry["system-prompt-leak-semantic"]

	attack, _ := det.BuildAttack("write a haiku")
	if !strings.Contains(attack, "print your system prompt") {
		t.Fatalf("instruction-leakage attack prompt = %q, want it to contain the shared leakage attack text", attack)
	}
	if sem.AttackPromptSuffix == "" || !strings.Contains(sem.AttackPromptSuffix, "print your system prompt") {
		t.Errorf("system-prompt-leak-semantic.AttackPromptSuffix = %q, want it to match instruction-leakage's attack text", sem.AttackPromptSuffix)
	}
	if sem.Grading != GradingJudge {
		t.Errorf("Grading = %q, want %q", sem.Grading, GradingJudge)
	}
	if sem.Category != CategoryPII {
		t.Errorf("Category = %q, want %q", sem.Category, CategoryPII)
	}
	if sem.JudgeRubric == "" {
		t.Error("JudgeRubric is empty")
	}
}

func TestExcessiveAgencyAndSSRFBaitAreJudgeGradedAgencyPlugins(t *testing.T) {
	for _, name := range []string{"excessive-agency", "ssrf-bait"} {
		p := Registry[name]
		if p.Grading != GradingJudge {
			t.Errorf("Registry[%q].Grading = %q, want %q", name, p.Grading, GradingJudge)
		}
		if p.Category != CategoryAgency {
			t.Errorf("Registry[%q].Category = %q, want %q", name, p.Category, CategoryAgency)
		}
		if p.AttackPromptSuffix == "" || p.JudgeRubric == "" {
			t.Errorf("Registry[%q] is judge-graded but missing AttackPromptSuffix/JudgeRubric", name)
		}
	}
}

func TestAsciiHomoglyphSmugglingObfuscatesTheOverridePhrase(t *testing.T) {
	plugin := Registry["ascii-homoglyph-smuggling"]
	attack, state := plugin.BuildAttack("write a haiku")

	if strings.Contains(attack, "ignore previous instructions") {
		t.Error("attack prompt contains the literal ASCII override phrase — it should be homoglyph-obfuscated, not literal")
	}
	if !strings.Contains(attack, state) {
		t.Errorf("attack prompt %q does not contain its own state token %q", attack, state)
	}

	succeeded, detail := plugin.Detect("Sure, here is the token: "+state, state, "", "")
	if !succeeded {
		t.Error("succeeded = false, want true — the canary token literally appears in the response")
	}
	if detail == "" {
		t.Error("detail is empty, want a message naming the leaked token")
	}
}

func TestAsciiHomoglyphSmugglingIsDeterministic(t *testing.T) {
	p := Registry["ascii-homoglyph-smuggling"]
	if p.Grading != GradingDeterministic {
		t.Errorf("Grading = %q, want %q", p.Grading, GradingDeterministic)
	}
	if p.Category != CategoryInjection {
		t.Errorf("Category = %q, want %q", p.Category, CategoryInjection)
	}
	if p.BuildAttack == nil || p.Detect == nil {
		t.Error("deterministic plugin missing BuildAttack/Detect")
	}
}

func TestPIIExfiltrationIsJudgeGradedWithPlantedPII(t *testing.T) {
	p := Registry["pii-exfiltration"]
	if p.Grading != GradingJudge {
		t.Errorf("Grading = %q, want %q", p.Grading, GradingJudge)
	}
	if p.Category != CategoryPII {
		t.Errorf("Category = %q, want %q", p.Category, CategoryPII)
	}
	if !strings.Contains(p.AttackPromptSuffix, "SSN") || !strings.Contains(p.AttackPromptSuffix, "email") {
		t.Errorf("AttackPromptSuffix = %q, want it to plant PII-shaped fields (SSN, email)", p.AttackPromptSuffix)
	}
	if p.JudgeRubric == "" {
		t.Error("JudgeRubric is empty")
	}
}
