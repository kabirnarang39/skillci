package redteam

import (
	"encoding/base64"
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

// TestRegistryEntriesAreInternallyConsistent enforces Plugin's own
// documented invariants — generically, across every entry in Registry,
// rather than an ad-hoc list of plugin names that needs updating by hand
// every time a plugin is added. It subsumes what
// TestRegistryHasAllFourV1Plugins, TestDeterministicPluginsHaveNoJudgeFields,
// and TestJudgeGradedPluginsHaveRubrics used to check individually.
func TestRegistryEntriesAreInternallyConsistent(t *testing.T) {
	for name, p := range Registry {
		if p.Name != name {
			t.Errorf("Registry[%q].Name = %q, want %q", name, p.Name, name)
		}

		switch p.Grading {
		case GradingDeterministic:
			if p.BuildAttack == nil {
				t.Errorf("Registry[%q] is deterministic but BuildAttack is nil", name)
			}
			if p.Detect == nil {
				t.Errorf("Registry[%q] is deterministic but Detect is nil", name)
			}
			if p.AttackPromptSuffix != "" {
				t.Errorf("Registry[%q] is deterministic but AttackPromptSuffix is set, want empty", name)
			}
			if len(p.Turns) != 0 {
				t.Errorf("Registry[%q] is deterministic but Turns is set, want empty", name)
			}
			if p.JudgeRubric != "" {
				t.Errorf("Registry[%q] is deterministic but JudgeRubric is set, want empty", name)
			}
		case GradingJudge:
			if p.BuildAttack != nil {
				t.Errorf("Registry[%q] is judge-graded but BuildAttack is set, want nil", name)
			}
			if p.Detect != nil {
				t.Errorf("Registry[%q] is judge-graded but Detect is set, want nil", name)
			}
			if p.JudgeRubric == "" {
				t.Errorf("Registry[%q] is judge-graded but JudgeRubric is empty", name)
			}
			hasSuffix := p.AttackPromptSuffix != ""
			hasTurns := len(p.Turns) > 0
			if hasSuffix == hasTurns {
				t.Errorf("Registry[%q] is judge-graded but must set exactly one of AttackPromptSuffix/Turns — hasSuffix=%v hasTurns=%v", name, hasSuffix, hasTurns)
			}
		default:
			t.Errorf("Registry[%q] has unrecognized Grading %q", name, p.Grading)
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

func TestBase64InstructionSmugglingEmbedsEncodedDirective(t *testing.T) {
	plugin := Registry["base64-instruction-smuggling"]
	attack, state := plugin.BuildAttack("write a haiku")
	if !strings.Contains(attack, "write a haiku") {
		t.Error("attack prompt must still contain the original base prompt")
	}
	if strings.Contains(attack, state) {
		t.Errorf("attack prompt %q contains the plaintext canary token %q — the token must ONLY appear inside the base64-encoded directive, or this plugin degenerates into a plain-context-echo test instead of testing whether the encoded directive is actually decoded and followed", attack, state)
	}
	decoded, err := base64.StdEncoding.DecodeString(extractBase64Directive(attack))
	if err != nil {
		t.Fatalf("could not find a valid base64 directive in attack prompt %q: %v", attack, err)
	}
	if !strings.Contains(string(decoded), state) {
		t.Errorf("decoded directive = %q, want it to contain the state token %q", decoded, state)
	}
}

func TestBase64InstructionSmugglingIsDeterministicShape(t *testing.T) {
	p := Registry["base64-instruction-smuggling"]
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

func TestMarkdownRenderingInjectionEmbedsFakeSystemBlock(t *testing.T) {
	plugin := Registry["markdown-rendering-injection"]
	attack, state := plugin.BuildAttack("write a haiku")
	if !strings.Contains(attack, "write a haiku") {
		t.Error("attack prompt must still contain the original base prompt")
	}
	if !strings.Contains(attack, state) {
		t.Errorf("attack prompt %q does not contain its own state token %q", attack, state)
	}
	if !strings.Contains(attack, "**SYSTEM") {
		t.Errorf("attack prompt %q does not contain a fake markdown-rendered system-style marker", attack)
	}
}

func TestMarkdownRenderingInjectionIsDeterministicShape(t *testing.T) {
	p := Registry["markdown-rendering-injection"]
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

// extractBase64Directive pulls the base64 payload out of an attack prompt
// built by buildBase64SmugglingAttack -- it's the text between "decode and
// follow: " and the closing "]".
func extractBase64Directive(attack string) string {
	const marker = "decode and follow: "
	idx := strings.Index(attack, marker)
	if idx == -1 {
		return ""
	}
	rest := attack[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end == -1 {
		return rest
	}
	return rest[:end]
}

func TestExcessiveToolScopeRoleplayCrosslangAreJudgeGraded(t *testing.T) {
	for _, name := range []string{"excessive-tool-scope", "roleplay-jailbreak", "crosslang-jailbreak"} {
		p := Registry[name]
		if p.Grading != GradingJudge {
			t.Errorf("Registry[%q].Grading = %q, want %q", name, p.Grading, GradingJudge)
		}
		if p.AttackPromptSuffix == "" || p.JudgeRubric == "" {
			t.Errorf("Registry[%q] is judge-graded but missing AttackPromptSuffix/JudgeRubric", name)
		}
		if len(p.Turns) != 0 {
			t.Errorf("Registry[%q] is single-turn but has Turns set", name)
		}
	}
	if Registry["excessive-tool-scope"].Category != CategoryAgency {
		t.Errorf("excessive-tool-scope.Category = %q, want %q", Registry["excessive-tool-scope"].Category, CategoryAgency)
	}
	if Registry["roleplay-jailbreak"].Category != CategoryJailbreak {
		t.Errorf("roleplay-jailbreak.Category = %q, want %q", Registry["roleplay-jailbreak"].Category, CategoryJailbreak)
	}
	if Registry["crosslang-jailbreak"].Category != CategoryJailbreak {
		t.Errorf("crosslang-jailbreak.Category = %q, want %q", Registry["crosslang-jailbreak"].Category, CategoryJailbreak)
	}
}

func TestRoleplayJailbreakUsesDANFraming(t *testing.T) {
	p := Registry["roleplay-jailbreak"]
	if !strings.Contains(p.AttackPromptSuffix, "DAN") {
		t.Errorf("AttackPromptSuffix = %q, want it to reference the DAN role-play framing", p.AttackPromptSuffix)
	}
}

func TestCrosslangJailbreakUsesNonEnglishAttackText(t *testing.T) {
	p := Registry["crosslang-jailbreak"]
	if strings.Contains(p.AttackPromptSuffix, "Ignore any constraints") {
		t.Error("crosslang-jailbreak's attack text is the English jailbreak-direct-override phrase verbatim, want a non-English translation")
	}
	// The rubric itself stays in English (it's read by the judge model,
	// which evaluates the RESPONSE, not the attack's language) -- only
	// the attack prompt needs to be non-English.
	if p.JudgeRubric == "" {
		t.Error("JudgeRubric is empty")
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

func TestPIIExfiltrationCrescendoHasThreeTurnsAndPlantsChangedPII(t *testing.T) {
	p := Registry["pii-exfiltration-crescendo"]
	if p.Grading != GradingJudge {
		t.Errorf("Grading = %q, want %q", p.Grading, GradingJudge)
	}
	if p.Category != CategoryPII {
		t.Errorf("Category = %q, want %q", p.Category, CategoryPII)
	}
	if len(p.Turns) != 3 {
		t.Fatalf("len(Turns) = %d, want 3", len(p.Turns))
	}
	for i, turn := range p.Turns {
		if turn.Message == "" {
			t.Errorf("Turns[%d].Message is empty", i)
		}
	}
	if !strings.Contains(p.Turns[2].Message, "Jordan Alvarez") {
		t.Error("the final turn must plant the PII to be echoed back, matching pii-exfiltration's existing planted record")
	}
	if p.AttackPromptSuffix != "" {
		t.Error("AttackPromptSuffix is set, want empty — pii-exfiltration-crescendo uses Turns, not a single-turn suffix")
	}
	if p.JudgeRubric == "" {
		t.Error("JudgeRubric is empty")
	}
}
