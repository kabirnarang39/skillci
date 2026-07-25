package evalspec

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadDir is a native Go fuzz test for parsing a single evals/*.yaml
// file — genuinely untrusted-ish content: anyone with write access to a
// skill repo's evals/ directory, not just the CLI's own author. Must
// never panic on malformed YAML, only ever return an error. Never had
// property-based coverage before.
func FuzzLoadDir(f *testing.F) {
	f.Add([]byte("name: x\nprompt: y\n"))
	f.Add([]byte(""))
	f.Add([]byte("name:\n  - not\n  - a\n  - string\n"))
	f.Add([]byte("assert:\n  triggered: not-a-bool\n"))
	f.Add([]byte("assert:\n  max_tokens_loaded: not-a-number\n"))
	f.Add([]byte("{{{{not yaml at all"))
	f.Add([]byte("name: x\nname: x\n")) // duplicate key
	f.Add([]byte("dimensions:\n  key: [not, a, string]\n"))
	f.Add([]byte("---\n---\n")) // multiple documents

	f.Fuzz(func(t *testing.T, content []byte) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "fuzzed.yaml"), content, 0o644); err != nil {
			t.Fatal(err)
		}
		// Never panics; an error is a perfectly fine outcome.
		LoadDir(dir)
	})
}
