package bisectcache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoadCorruptJSONReturnsError covers a bisect-cache.json that exists
// but isn't valid JSON — a plausible scenario for a git-committed file
// (unresolved merge conflict, truncated write). Previously untested.
func TestLoadCorruptJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bisect-cache.json")
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
	if err := c.Save(filepath.Join(dir, "bisect-cache.json")); err == nil {
		t.Error("Save() error = nil, want an error writing to a read-only directory")
	}
}

func TestLoadMissingFileReturnsEmptyCache(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "bisect-cache.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(c.Entries) != 0 {
		t.Errorf("Entries = %v, want empty", c.Entries)
	}
}

func TestResultMissReturnsFalseFalse(t *testing.T) {
	c := Cache{}
	if _, ok := c.Result("case", "model", "sha"); ok {
		t.Error("Result() ok = true, want false for empty cache")
	}
}

func TestRecordThenResultRoundTrips(t *testing.T) {
	var c Cache
	c.Record("case1", "claude-sonnet-5", "abc123", true)
	passed, ok := c.Result("case1", "claude-sonnet-5", "abc123")
	if !ok || !passed {
		t.Errorf("Result() = %v, %v, want true, true", passed, ok)
	}
}

func TestResultDoesNotCrossCaseOrModelBoundaries(t *testing.T) {
	var c Cache
	c.Record("case1", "claude-sonnet-5", "abc123", true)
	if _, ok := c.Result("case2", "claude-sonnet-5", "abc123"); ok {
		t.Error("Result() matched a different case name")
	}
	if _, ok := c.Result("case1", "claude-opus-4-8", "abc123"); ok {
		t.Error("Result() matched a different model")
	}
}

func TestRecordUpdatesExistingEntryInPlaceRatherThanDuplicating(t *testing.T) {
	var c Cache
	c.Record("case1", "claude-sonnet-5", "abc123", false)
	c.Record("case1", "claude-sonnet-5", "abc123", true)
	if len(c.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1 (re-recording the same key should update, not append)", len(c.Entries))
	}
	passed, ok := c.Result("case1", "claude-sonnet-5", "abc123")
	if !ok || !passed {
		t.Errorf("Result() = %v, %v, want true, true after update", passed, ok)
	}
}

func TestRecordCapsAtMaxRetainedEntriesDroppingOldest(t *testing.T) {
	var c Cache
	for i := range maxRetainedEntries + 10 {
		c.Record("case1", "claude-sonnet-5", string(rune('a'))+string(rune(i)), true)
	}
	if len(c.Entries) != maxRetainedEntries {
		t.Fatalf("Entries = %d, want %d", len(c.Entries), maxRetainedEntries)
	}
	// the very first sha recorded should have been evicted
	if _, ok := c.Result("case1", "claude-sonnet-5", string(rune('a'))+string(rune(0))); ok {
		t.Error("Result() found the oldest entry, want it evicted by the cap")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bisect-cache.json")
	var c Cache
	c.Record("case1", "claude-sonnet-5", "abc123", true)
	c.Record("case1", "claude-sonnet-5", "def456", false)
	if err := c.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if passed, ok := loaded.Result("case1", "claude-sonnet-5", "abc123"); !ok || !passed {
		t.Errorf("Result(abc123) = %v, %v, want true, true", passed, ok)
	}
	if passed, ok := loaded.Result("case1", "claude-sonnet-5", "def456"); !ok || passed {
		t.Errorf("Result(def456) = %v, %v, want false, true", passed, ok)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".skillci", "bisect-cache.json")
	var c Cache
	c.Record("case1", "claude-sonnet-5", "abc123", true)
	if err := c.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
}
