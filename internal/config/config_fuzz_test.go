package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoad is a native Go fuzz test for parsing .skillci.yaml — must never
// panic on malformed content, only ever return an error. Never had
// property-based coverage before.
func FuzzLoad(f *testing.F) {
	f.Add([]byte("models: [claude-sonnet-5]\nfail_on: regression\n"))
	f.Add([]byte(""))
	f.Add([]byte("fail_on: not-a-real-policy\n"))
	f.Add([]byte("pricing:\n  x:\n    input_per_million: not-a-number\n"))
	f.Add([]byte("pricing:\n  x:\n    input_per_million: -5\n"))
	f.Add([]byte("models: not-a-list\n"))
	f.Add([]byte("strict_dimensions:\n  key: not-a-list\n"))
	f.Add([]byte("{{{{not yaml at all"))
	f.Add([]byte("fail_on:\n  - not\n  - a\n  - string\n"))

	f.Fuzz(func(t *testing.T, content []byte) {
		path := filepath.Join(t.TempDir(), ".skillci.yaml")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		// Never panics; an error is a perfectly fine outcome.
		Load(path)
	})
}
