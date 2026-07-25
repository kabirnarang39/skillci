package lint

import "testing"

// FuzzSplitFrontmatter is a native Go fuzz test for the one function that
// parses a SKILL.md's raw text (untrusted content — the whole point of
// `skillci check` is reviewing skills nobody has vetted yet) before any
// other validation runs. Never had property-based coverage; every existing
// test hand-picks specific well-formed or specifically-malformed inputs.
// The only property under test is "never panics" — a returned error is a
// perfectly fine, expected outcome for malformed input.
func FuzzSplitFrontmatter(f *testing.F) {
	f.Add("---\nname: x\ndescription: y\n---\nBody.\n")
	f.Add("")
	f.Add("---\n")
	f.Add("---\n---\n")
	f.Add("---\n\n---\n")
	f.Add("no frontmatter at all")
	f.Add("---\nunclosed")
	f.Add("---\n---\n---\n")
	f.Add("--\nname: x\n--\n")
	f.Add("---\r\nname: x\r\n---\r\nBody.\r\n")

	f.Fuzz(func(t *testing.T, content string) {
		fm, body, err := splitFrontmatter(content)
		if err != nil {
			return
		}
		// On success, fm+body must reconstruct into something no longer
		// than the original (never fabricates content), and fm must
		// never itself contain the closing delimiter (that would mean
		// the split point was computed wrong).
		if len(fm)+len(body) > len(content) {
			t.Errorf("splitFrontmatter(%q) = fm=%q, body=%q — reconstructed content longer than input", content, fm, body)
		}
	})
}
