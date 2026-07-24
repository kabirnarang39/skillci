package fuzz

import "testing"

func TestSynonymSwap(t *testing.T) {
	muts := Generate("Can you write me a haiku about autumn?")
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

func TestSynonymSwapNoHitReturnsNoSynonymMutation(t *testing.T) {
	muts := Generate("What is the weather like today?")
	for _, m := range muts {
		if m.Operator == "synonym-swap" {
			t.Errorf("Generate() produced synonym-swap mutation %+v for a prompt with no synonym-map hit", m)
		}
	}
}

func TestNegationInsertion(t *testing.T) {
	muts := Generate("Can you write me a haiku about autumn?")
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
	muts := Generate("Write me a haiku about autumn.")
	for _, m := range muts {
		if m.Operator == "reorder" {
			t.Errorf("Generate() produced a reorder mutation %+v for a single-sentence prompt", m)
		}
	}
}

func TestSentenceReorderTwoSentences(t *testing.T) {
	muts := Generate("Write me a haiku. Make it about autumn.")
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
	muts := Generate("One. Two. Three. Four.")
	for _, m := range muts {
		if m.Operator == "reorder" {
			t.Errorf("Generate() produced a reorder mutation %+v for a 4-sentence prompt, want skipped (combinatorial blowup guard)", m)
		}
	}
}

func TestContextPrefix(t *testing.T) {
	muts := Generate("Write me a haiku about autumn.")
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
	if muts := Generate(""); len(muts) != 0 {
		t.Errorf("Generate(\"\") = %+v, want empty slice", muts)
	}
	if muts := Generate("   "); len(muts) != 0 {
		t.Errorf("Generate(whitespace) = %+v, want empty slice", muts)
	}
}

func TestGenerateUnmutatablePromptStillReturnsContextPrefixOnly(t *testing.T) {
	// A short, single-sentence prompt with no synonym-map hits and no
	// verb-prefix pattern still gets negation (word-0 heuristic) and
	// context-prefix mutations — Generate never returns nil for any
	// non-empty prompt.
	muts := Generate("Autumn.")
	if len(muts) == 0 {
		t.Error("Generate(\"Autumn.\") returned no mutations, want at least negation+context-prefix")
	}
}
