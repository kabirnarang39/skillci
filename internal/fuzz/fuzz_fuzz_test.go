package fuzz

import (
	"strings"
	"testing"
)

// FuzzGenerate is a native Go fuzz test for Generate itself — this
// package's whole job is turning arbitrary user-authored eval prompts into
// mutations, so it needs to survive arbitrary prompt text without
// panicking (an index error in splitSentences or permute, say, would take
// down an entire regress run over one adversarial prompt in someone's
// evals/*.yaml). Never had property-based coverage before.
func FuzzGenerate(f *testing.F) {
	f.Add("Can you review this PR for SOLID violations?")
	f.Add("")
	f.Add("   ")
	f.Add(".")
	f.Add("...")
	f.Add("One. Two. Three. Four. Five.")
	f.Add("please review and fix this and make and create and check")
	f.Add(strings.Repeat("word ", 500))
	f.Add("weird\x00null\x01bytes")
	f.Add("unicode: 日本語 emoji: 🎉🎉🎉")
	f.Add("Please REVIEW and FIX this.")
	f.Add("?!.?!.?!.")

	f.Fuzz(func(t *testing.T, prompt string) {
		muts, _ := Generate(prompt)
		for _, m := range muts {
			if m.Prompt == "" && strings.TrimSpace(prompt) != "" {
				t.Errorf("Generate(%q) produced a mutation with an empty Prompt: %+v", prompt, m)
			}
		}
	})
}
