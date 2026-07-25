package snapshot

import (
	"strings"
	"testing"
)

// FuzzCompute is a native Go fuzz test for the word-level LCS diff — a
// hand-rolled O(n*m) DP with manual index arithmetic in both the table
// fill and the reconstruction walk (diffWords), then more index slicing in
// renderOps' ellipsis-collapsing. Exactly the kind of code an off-by-one
// turns into an index-out-of-range panic on some input nobody happened to
// try by hand. oldText/newText are real model output, so effectively
// arbitrary text. Never had property-based coverage before.
func FuzzCompute(f *testing.F) {
	f.Add("Old leaves drift and fall.", "Old leaves drift and settle.")
	f.Add("", "")
	f.Add("", "some text")
	f.Add("some text", "")
	f.Add("same", "same")
	f.Add("a a a a a a a a a a", "a a a a a a a a a a")
	f.Add("a a a a a a a a a a", "b b b b b b b b b b")
	f.Add(strings.Repeat("word ", 200), strings.Repeat("other ", 200))
	f.Add("one two three four five six seven", "seven six five four three two one")
	f.Add("   ", "\t\n\t")
	f.Add("unicode 日本語", "unicode 中文")
	f.Add("a", "a a a a a a a a a a a a a a a a a a a a")

	f.Fuzz(func(t *testing.T, oldText, newText string) {
		d := Compute(oldText, newText)
		if oldText == newText && d.Changed {
			// Not strictly guaranteed for whitespace-only differences
			// (Normalize handles that), but identical strings must never
			// register as changed.
			t.Errorf("Compute(%q, %q): Changed = true for identical text", oldText, newText)
		}
		if d.Changed && d.Render == "" {
			t.Errorf("Compute(%q, %q): Changed = true but Render is empty", oldText, newText)
		}
	})
}
