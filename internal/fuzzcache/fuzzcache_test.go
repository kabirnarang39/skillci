package fuzzcache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoadCorruptJSONReturnsError covers a fuzz-llm-cache.json that
// exists but isn't valid JSON — a plausible scenario for a git-committed
// file (unresolved merge conflict, truncated write). Previously untested.
func TestLoadCorruptJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fuzz-llm-cache.json")
	if err := os.WriteFile(path, []byte("<<<<<<< HEAD\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load() error = nil, want an error for corrupt JSON content")
	}
}

// TestSaveToUnwritableDirectoryReturnsError covers os.WriteFile itself
// failing. Skipped on Windows, where chmod-based read-only directories
// don't block writes the same way.
func TestSaveToUnwritableDirectoryReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions don't block writes the same way on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	c := Cache{}
	if err := c.Save(filepath.Join(dir, "fuzz-llm-cache.json")); err == nil {
		t.Error("Save() error = nil, want an error writing to a read-only directory")
	}
}

func TestHashPromptIsStableAndDistinguishesContent(t *testing.T) {
	h1 := HashPrompt("write a haiku")
	h2 := HashPrompt("write a haiku")
	h3 := HashPrompt("write a sonnet")
	if h1 != h2 {
		t.Errorf("HashPrompt() not stable: %q != %q for the same input", h1, h2)
	}
	if h1 == h3 {
		t.Error("HashPrompt() collided for different prompts")
	}
}

func TestLoadMissingFileReturnsEmptyCache(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "fuzz-llm-cache.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(c.Entries) != 0 {
		t.Errorf("Entries = %v, want empty", c.Entries)
	}
}

func TestParaphrasesMissReturnsFalse(t *testing.T) {
	var c Cache
	if _, ok := c.Paraphrases("hash"); ok {
		t.Error("Paraphrases() ok = true, want false for empty cache")
	}
}

func TestRecordThenParaphrasesRoundTrips(t *testing.T) {
	var c Cache
	want := []string{"paraphrase one", "paraphrase two"}
	c.Record("hash1", want)
	got, ok := c.Paraphrases("hash1")
	if !ok {
		t.Fatal("Paraphrases() ok = false, want true after Record")
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Paraphrases() = %v, want %v", got, want)
	}
}

func TestRecordUpdatesExistingEntryInPlaceRatherThanDuplicating(t *testing.T) {
	var c Cache
	c.Record("hash1", []string{"old"})
	c.Record("hash1", []string{"new"})
	if len(c.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1 (re-recording the same hash should update, not append)", len(c.Entries))
	}
	got, _ := c.Paraphrases("hash1")
	if len(got) != 1 || got[0] != "new" {
		t.Errorf("Paraphrases() = %v, want [new]", got)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fuzz-llm-cache.json")
	var c Cache
	c.Record("hash1", []string{"a", "b"})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := loaded.Paraphrases("hash1")
	if !ok || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Paraphrases() after round-trip = %v, %v, want [a b], true", got, ok)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".skillci", "fuzz-llm-cache.json")
	var c Cache
	c.Record("hash1", []string{"a"})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
}

func TestRecordCapsAtMaxRetainedEntriesDroppingOldest(t *testing.T) {
	var c Cache
	for i := range maxRetainedEntries + 10 {
		c.Record(HashPrompt(string(rune(i))), []string{"p"})
	}
	if len(c.Entries) != maxRetainedEntries {
		t.Fatalf("Entries = %d, want %d", len(c.Entries), maxRetainedEntries)
	}
	if _, ok := c.Paraphrases(HashPrompt(string(rune(0)))); ok {
		t.Error("Paraphrases() found the oldest entry, want it evicted by the cap")
	}
}
