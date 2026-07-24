package bisectcache

import (
	"path/filepath"
	"testing"
)

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
