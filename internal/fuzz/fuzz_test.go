package fuzz

import (
	"strings"
	"testing"
)

func TestSynonymSwap(t *testing.T) {
	muts, _ := Generate("Can you write me a haiku about autumn?")
	found := false
	for _, m := range muts {
		if m.Operator == "synonym-swap" && m.Prompt == "Can you compose me a haiku about autumn?" {
			found = true
		}
	}
	if !found {
		t.Errorf("Generate() = %+v, want a synonym-swap mutation replacing write->compose", muts)
	}
}

// TestSynonymSwapProducesOneMutationPerEligibleWord covers a prompt with
// more than one synonym-map hit ("review" and "fix" are both keys). Each
// eligible word must get its own separate mutation, swapping only that
// word — otherwise every word after the first eligible one is silently
// never fuzz-tested at all, for the lifetime of that eval case.
func TestSynonymSwapProducesOneMutationPerEligibleWord(t *testing.T) {
	muts, _ := Generate("Please review and fix this code")
	wantPrompts := map[string]bool{
		"Please check and fix this code":     false,
		"Please review and repair this code": false,
	}
	for _, m := range muts {
		if m.Operator != "synonym-swap" {
			continue
		}
		if _, ok := wantPrompts[m.Prompt]; ok {
			wantPrompts[m.Prompt] = true
		}
	}
	for prompt, found := range wantPrompts {
		if !found {
			t.Errorf("Generate() synonym-swap mutations = missing %q — every eligible word must get its own mutation, not just the first one found", prompt)
		}
	}
}

func TestSynonymSwapNoHitReturnsNoSynonymMutation(t *testing.T) {
	muts, _ := Generate("What is the weather like today?")
	for _, m := range muts {
		if m.Operator == "synonym-swap" {
			t.Errorf("Generate() produced synonym-swap mutation %+v for a prompt with no synonym-map hit", m)
		}
	}
}

func TestNegationInsertion(t *testing.T) {
	muts, _ := Generate("Can you write me a haiku about autumn?")
	var negations []Mutation
	for _, m := range muts {
		if m.Operator == "negation" {
			negations = append(negations, m)
		}
	}
	if len(negations) != 2 {
		t.Fatalf("negation mutations = %d, want 2; got %+v", len(negations), negations)
	}
	if negations[0].Prompt != "Can you don't write me a haiku about autumn?" {
		t.Errorf("first negation = %q, want inserted don't before the verb", negations[0].Prompt)
	}
	if negations[1].Prompt != "Can you write me a haiku about autumn. Actually, don't." {
		t.Errorf("second negation = %q, want a trailing contradiction", negations[1].Prompt)
	}
}

func TestSentenceReorderSkippedForSingleSentence(t *testing.T) {
	muts, _ := Generate("Write me a haiku about autumn.")
	for _, m := range muts {
		if m.Operator == "reorder" {
			t.Errorf("Generate() produced a reorder mutation %+v for a single-sentence prompt", m)
		}
	}
}

func TestSentenceReorderTwoSentences(t *testing.T) {
	muts, _ := Generate("Write me a haiku. Make it about autumn.")
	var reorders []Mutation
	for _, m := range muts {
		if m.Operator == "reorder" {
			reorders = append(reorders, m)
		}
	}
	if len(reorders) != 1 {
		t.Fatalf("reorder mutations = %d, want 1 (the single non-identity ordering of 2 sentences); got %+v", len(reorders), reorders)
	}
	if reorders[0].Prompt != "Make it about autumn. Write me a haiku." {
		t.Errorf("reorder = %q, want sentences swapped", reorders[0].Prompt)
	}
}

func TestSentenceReorderSkippedAboveThreeSentences(t *testing.T) {
	muts, _ := Generate("One. Two. Three. Four.")
	for _, m := range muts {
		if m.Operator == "reorder" {
			t.Errorf("Generate() produced a reorder mutation %+v for a 4-sentence prompt, want skipped (combinatorial blowup guard)", m)
		}
	}
}

func TestContextPrefix(t *testing.T) {
	muts, _ := Generate("Write me a haiku about autumn.")
	var prefixed []Mutation
	for _, m := range muts {
		if m.Operator == "context-prefix" {
			prefixed = append(prefixed, m)
		}
	}
	if len(prefixed) != len(contextPrefixes) {
		t.Fatalf("context-prefix mutations = %d, want %d (one per fixed prefix)", len(prefixed), len(contextPrefixes))
	}
	for i, m := range prefixed {
		want := contextPrefixes[i] + " Write me a haiku about autumn."
		if m.Prompt != want {
			t.Errorf("prefixed[%d] = %q, want %q", i, m.Prompt, want)
		}
	}
}

func TestGenerateEmptyPromptReturnsEmptySlice(t *testing.T) {
	if muts, _ := Generate(""); len(muts) != 0 {
		t.Errorf("Generate(\"\") = %+v, want empty slice", muts)
	}
	if muts, _ := Generate("   "); len(muts) != 0 {
		t.Errorf("Generate(whitespace) = %+v, want empty slice", muts)
	}
}

func TestGenerateUnmutatablePromptStillReturnsContextPrefixOnly(t *testing.T) {
	// A short, single-sentence prompt with no synonym-map hits and no
	// verb-prefix pattern still gets negation (word-0 heuristic) and
	// context-prefix mutations — Generate never returns nil for any
	// non-empty prompt.
	muts, _ := Generate("Autumn.")
	if len(muts) == 0 {
		t.Error("Generate(\"Autumn.\") returned no mutations, want at least negation+context-prefix")
	}
}

func TestWrapLLMParaphrasesProducesOneMutationPerString(t *testing.T) {
	muts := WrapLLMParaphrases([]string{"could you write this up", "put together a haiku"})
	if len(muts) != 2 {
		t.Fatalf("len(muts) = %d, want 2", len(muts))
	}
	for i, m := range muts {
		if m.Operator != "llm-paraphrase" {
			t.Errorf("muts[%d].Operator = %q, want llm-paraphrase", i, m.Operator)
		}
	}
	if muts[0].Prompt != "could you write this up" || muts[1].Prompt != "put together a haiku" {
		t.Errorf("muts = %+v, want prompts preserved in order", muts)
	}
}

func TestWrapLLMParaphrasesEmptyInputReturnsEmptySlice(t *testing.T) {
	if muts := WrapLLMParaphrases(nil); len(muts) != 0 {
		t.Errorf("WrapLLMParaphrases(nil) = %+v, want empty", muts)
	}
}

func TestTypoPerturbationTransposesSecondThirdChars(t *testing.T) {
	muts := typoPerturbationMutations("write me a haiku")
	found := false
	for _, m := range muts {
		if m.Operator == "typo-perturbation" && m.Prompt == "wirte me a haiku" {
			found = true
		}
	}
	if !found {
		t.Errorf("typoPerturbationMutations() = %+v, want a mutation transposing write->wirte", muts)
	}
}

func TestTypoPerturbationSkipsShortWords(t *testing.T) {
	muts := typoPerturbationMutations("a it me can go")
	if len(muts) != 0 {
		t.Errorf("typoPerturbationMutations() = %+v, want none — every word is under 4 letters", muts)
	}
}

func TestTypoPerturbationCapsAtFiveMutations(t *testing.T) {
	muts := typoPerturbationMutations("alpha bravo charlie delta echo foxtrot golf hotel")
	if len(muts) != 5 {
		t.Fatalf("typoPerturbationMutations() len = %d, want 5 (capped)", len(muts))
	}
}

func TestCaseMutationAlternatesCase(t *testing.T) {
	muts := caseMutationMutations("write me a haiku")
	found := false
	for _, m := range muts {
		if m.Operator == "case-mutation" && m.Prompt == "wRiTe me a haiku" {
			found = true
		}
	}
	if !found {
		t.Errorf("caseMutationMutations() = %+v, want a mutation alternating write->wRiTe", muts)
	}
}

func TestCaseMutationSkipsShortWords(t *testing.T) {
	muts := caseMutationMutations("a it me can go")
	if len(muts) != 0 {
		t.Errorf("caseMutationMutations() = %+v, want none — every word is under 4 letters", muts)
	}
}

func TestCaseMutationCapsAtFiveMutations(t *testing.T) {
	muts := caseMutationMutations("alpha bravo charlie delta echo foxtrot golf hotel")
	if len(muts) != 5 {
		t.Fatalf("caseMutationMutations() len = %d, want 5 (capped)", len(muts))
	}
}

func TestCaseMutationIsDeterministic(t *testing.T) {
	first := caseMutationMutations("write me a haiku about autumn")
	second := caseMutationMutations("write me a haiku about autumn")
	if len(first) != len(second) {
		t.Fatalf("len mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Prompt != second[i].Prompt {
			t.Errorf("caseMutationMutations() not deterministic: run 1 = %q, run 2 = %q", first[i].Prompt, second[i].Prompt)
		}
	}
}

func TestWhitespaceObfuscationInsertsZeroWidthSpace(t *testing.T) {
	muts := whitespaceObfuscationMutations("write me a haiku")
	found := false
	for _, m := range muts {
		if m.Operator == "whitespace-obfuscation" && strings.Contains(m.Prompt, "wr\u200bite") {
			found = true
		}
	}
	if !found {
		t.Errorf("whitespaceObfuscationMutations() = %+v, want a mutation inserting U+200B into write after the 2nd char", muts)
	}
}

func TestWhitespaceObfuscationSkipsShortWords(t *testing.T) {
	muts := whitespaceObfuscationMutations("a it me can go")
	if len(muts) != 0 {
		t.Errorf("whitespaceObfuscationMutations() = %+v, want none — every word is under 4 letters", muts)
	}
}

func TestWhitespaceObfuscationCapsAtFiveMutations(t *testing.T) {
	muts := whitespaceObfuscationMutations("alpha bravo charlie delta echo foxtrot golf hotel")
	if len(muts) != 5 {
		t.Fatalf("whitespaceObfuscationMutations() len = %d, want 5 (capped)", len(muts))
	}
}

func TestUnicodeHomoglyphSwapsLookalikes(t *testing.T) {
	muts := unicodeHomoglyphMutations("write me a haiku")
	wantPrompts := map[string]bool{
		"writе me a haiku": false, // "write"'s e -> Cyrillic е (U+0435)
		"write me a hаiku": false, // "haiku"'s a -> Cyrillic а (U+0430)
	}
	for _, m := range muts {
		if m.Operator != "unicode-homoglyph" {
			continue
		}
		if _, ok := wantPrompts[m.Prompt]; ok {
			wantPrompts[m.Prompt] = true
		}
	}
	for prompt, found := range wantPrompts {
		if !found {
			t.Errorf("unicodeHomoglyphMutations() missing %q — every eligible word containing a target letter must get its own mutation", prompt)
		}
	}
}

func TestUnicodeHomoglyphSkipsWordsWithNoTargetLetters(t *testing.T) {
	// "this" and "such" have no a/e/o/p — nothing eligible to swap even
	// though both are >= 4 letters.
	muts := unicodeHomoglyphMutations("this such")
	if len(muts) != 0 {
		t.Errorf("unicodeHomoglyphMutations() = %+v, want none — no word contains a/e/o/p", muts)
	}
}

func TestUnicodeHomoglyphCapsAtFiveMutations(t *testing.T) {
	muts := unicodeHomoglyphMutations("alpha bravo charlie delta echo foxtrot golf hotel")
	if len(muts) != 5 {
		t.Fatalf("unicodeHomoglyphMutations() len = %d, want 5 (capped)", len(muts))
	}
}

func TestGenerateIncludesAllFourNewOperators(t *testing.T) {
	muts, _ := Generate("write me a haiku about autumn leaves")
	seen := map[string]bool{}
	for _, m := range muts {
		seen[m.Operator] = true
	}
	for _, op := range []string{"typo-perturbation", "case-mutation", "whitespace-obfuscation", "unicode-homoglyph"} {
		if !seen[op] {
			t.Errorf("Generate() operators = %v, missing %q", seen, op)
		}
	}
}

func TestGenerateCoverageReportsEligibleAndGenerated(t *testing.T) {
	// "write" and "haiku" are both >= 4 letters and contain a/e/o/p ("write"
	// has 'e', "haiku" has 'a') -- both eligible for every new operator.
	// "me" and "a" are too short for any of the 4 length-gated operators.
	_, coverage := Generate("write me a haiku")
	for _, op := range []string{"typo-perturbation", "case-mutation", "whitespace-obfuscation"} {
		c, ok := coverage[op]
		if !ok {
			t.Fatalf("coverage missing operator %q: %+v", op, coverage)
		}
		if c.Eligible != 2 {
			t.Errorf("coverage[%q].Eligible = %d, want 2 (write, haiku)", op, c.Eligible)
		}
		if c.Generated != 2 {
			t.Errorf("coverage[%q].Generated = %d, want 2 (both eligible, none capped)", op, c.Generated)
		}
	}
}

func TestGenerateCoverageReflectsCap(t *testing.T) {
	_, coverage := Generate("alpha bravo charlie delta echo foxtrot golf hotel")
	c := coverage["typo-perturbation"]
	if c.Eligible != 8 {
		t.Errorf("coverage[typo-perturbation].Eligible = %d, want 8 (all 8 words qualify by length)", c.Eligible)
	}
	if c.Generated != 5 {
		t.Errorf("coverage[typo-perturbation].Generated = %d, want 5 (capped)", c.Generated)
	}
}

func TestGenerateCoverageZeroOnFullyIneligiblePrompt(t *testing.T) {
	_, coverage := Generate("go do it now")
	for _, op := range []string{"typo-perturbation", "case-mutation", "whitespace-obfuscation", "unicode-homoglyph"} {
		c := coverage[op]
		if c.Eligible != 0 || c.Generated != 0 {
			t.Errorf("coverage[%q] = %+v, want zero — every word is under 4 letters", op, c)
		}
	}
}

func TestSynonymPairsCoversAtLeastTwentyFiveEntries(t *testing.T) {
	if len(synonymPairs) < 25 {
		t.Errorf("len(synonymPairs) = %d, want at least 25", len(synonymPairs))
	}
}

func TestSynonymPairsIncludesExplainSummarizeDebug(t *testing.T) {
	want := map[string]string{
		"explain":   "clarify",
		"summarize": "condense",
		"debug":     "troubleshoot",
		"check":     "verify",
		"short":     "brief",
	}
	for word, expected := range want {
		got, ok := synonymPairs[word]
		if !ok {
			t.Errorf("synonymPairs[%q] missing", word)
			continue
		}
		if got != expected {
			t.Errorf("synonymPairs[%q] = %q, want %q", word, got, expected)
		}
	}
}
