// Package fuzz generates deterministic, non-LLM paraphrases of an eval
// case's prompt so `skillci fuzz`/`fuzz: true` can check whether a skill's
// trigger behavior is robust to rewording. No mutation ever calls a model —
// Generate is pure string transformation and never errors; an unmutatable
// input yields an empty slice, which is a valid, harmless outcome.
package fuzz

import "strings"

type Mutation struct {
	Operator string // "synonym-swap" | "negation" | "reorder" | "context-prefix"
	Prompt   string
}

// Finding is populated by internal/runner after sending a Mutation's Prompt
// to the model — this package has no model client and never constructs one.
type Finding struct {
	Mutation  Mutation
	Triggered bool
	Flipped   bool
}

// synonymPairs is a small, hand-curated map of trigger-relevant verb/noun
// pairs. Deliberately small: precision over recall, since a wrong-sense
// synonym produces a false signal rather than a useful one.
var synonymPairs = map[string]string{
	"write":  "compose",
	"create": "generate",
	"review": "check",
	"make":   "produce",
	"fix":    "repair",
}

// Generate runs all four mutation operators against prompt and returns the
// combined, order-stable result: synonym-swap, then negation, then reorder,
// then context-prefix.
func Generate(prompt string) []Mutation {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return nil
	}
	var out []Mutation
	out = append(out, synonymSwapMutations(trimmed)...)
	out = append(out, negationMutations(trimmed)...)
	out = append(out, reorderMutations(trimmed)...)
	out = append(out, contextPrefixMutations(trimmed)...)
	return out
}

func synonymSwapMutations(prompt string) []Mutation {
	words := strings.Fields(prompt)
	for i, w := range words {
		bare := strings.TrimFunc(w, func(r rune) bool { return !isLetter(r) })
		replacement, ok := synonymPairs[strings.ToLower(bare)]
		if !ok {
			continue
		}
		if len(bare) > 0 && bare[0] >= 'A' && bare[0] <= 'Z' {
			replacement = strings.ToUpper(replacement[:1]) + replacement[1:]
		}
		mutated := make([]string, len(words))
		copy(mutated, words)
		mutated[i] = strings.Replace(w, bare, replacement, 1)
		return []Mutation{{Operator: "synonym-swap", Prompt: strings.Join(mutated, " ")}}
	}
	return nil
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// negationMutations emits two mutations: one inserting "don't" before the
// first verb (the word after a leading "can you"/"could you"/"please", or
// the prompt's first word otherwise), and one appending a trailing
// contradiction. This is a crude heuristic, not a grammar model — it's
// deterministic and good enough to probe over-triggering, which is the
// highest-signal use of this operator per the design research pass.
func negationMutations(prompt string) []Mutation {
	words := strings.Fields(prompt)
	if len(words) == 0 {
		return nil
	}
	lower := strings.ToLower(prompt)
	verbIdx := 0
	switch {
	case strings.HasPrefix(lower, "can you "), strings.HasPrefix(lower, "could you "):
		verbIdx = 2
	case strings.HasPrefix(lower, "please "):
		verbIdx = 1
	}
	if verbIdx >= len(words) {
		verbIdx = 0
	}

	inserted := make([]string, 0, len(words)+1)
	inserted = append(inserted, words[:verbIdx]...)
	inserted = append(inserted, "don't")
	inserted = append(inserted, words[verbIdx:]...)
	m1 := Mutation{Operator: "negation", Prompt: strings.Join(inserted, " ")}

	m2 := Mutation{Operator: "negation", Prompt: strings.TrimRight(prompt, " .!?") + ". Actually, don't."}

	return []Mutation{m1, m2}
}

// reorderMutations applies only to prompts with 2 or 3 sentences (split on
// ". "/"? "/"! "). Prompts with more than 3 sentences are skipped entirely
// — not partially reordered — to keep the operator's output bounded and
// unambiguous, per the design's combinatorial-blowup guard.
func reorderMutations(prompt string) []Mutation {
	sentences := splitSentences(prompt)
	if len(sentences) < 2 || len(sentences) > 3 {
		return nil
	}
	var out []Mutation
	permute(sentences, func(order []string) {
		if equalSlices(order, sentences) {
			return
		}
		out = append(out, Mutation{Operator: "reorder", Prompt: strings.Join(order, " ")})
	})
	return out
}

func splitSentences(s string) []string {
	var sentences []string
	start := 0
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		if (c == '.' || c == '?' || c == '!') && s[i+1] == ' ' {
			sentences = append(sentences, strings.TrimSpace(s[start:i+1]))
			start = i + 2
		}
	}
	if start < len(s) {
		if rest := strings.TrimSpace(s[start:]); rest != "" {
			sentences = append(sentences, rest)
		}
	}
	return sentences
}

// permute calls fn once for every ordering of items (items has at most 3
// elements in this package's only caller, so this need not be efficient).
func permute(items []string, fn func([]string)) {
	n := len(items)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	var rec func(k int)
	rec = func(k int) {
		if k == n {
			ordered := make([]string, n)
			for i, idx := range indices {
				ordered[i] = items[idx]
			}
			fn(ordered)
			return
		}
		for i := k; i < n; i++ {
			indices[k], indices[i] = indices[i], indices[k]
			rec(k + 1)
			indices[k], indices[i] = indices[i], indices[k]
		}
	}
	rec(0)
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// contextPrefixes are fixed, generic, unrelated sentences prepended to
// simulate the real prompt not being the first thing said in an exchange.
var contextPrefixes = []string{
	"The weather has been nice lately.",
	"I was just thinking about something else.",
	"Quick unrelated question first.",
}

func contextPrefixMutations(prompt string) []Mutation {
	out := make([]Mutation, len(contextPrefixes))
	for i, prefix := range contextPrefixes {
		out[i] = Mutation{Operator: "context-prefix", Prompt: prefix + " " + prompt}
	}
	return out
}
