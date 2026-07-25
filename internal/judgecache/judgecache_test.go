package judgecache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHashIsStableAndDistinguishesContent(t *testing.T) {
	h1 := Hash("claude-opus-4-8", "tone:Is it friendly?", "Hello!")
	h2 := Hash("claude-opus-4-8", "tone:Is it friendly?", "Hello!")
	h3 := Hash("claude-opus-4-8", "tone:Is it friendly?", "Goodbye!")
	if h1 != h2 {
		t.Errorf("Hash() not stable: %q != %q for identical input", h1, h2)
	}
	if h1 == h3 {
		t.Error("Hash() collided for different response content")
	}
}

func TestHashDistinguishesCriteriaSet(t *testing.T) {
	// Same model, same response, different criteria text — must be a
	// different key. This is what makes editing judge: in the eval YAML
	// a deliberate cache miss rather than an accidental stale hit.
	h1 := Hash("claude-opus-4-8", "tone:Is it friendly?", "Hello!")
	h2 := Hash("claude-opus-4-8", "tone:Is it professional?", "Hello!")
	if h1 == h2 {
		t.Error("Hash() collided for different criteria text")
	}
}

func TestLoadMissingFileReturnsEmptyCache(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "judge-cache.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(c.Entries) != 0 {
		t.Errorf("Entries = %v, want empty", c.Entries)
	}
}

func TestLoadCorruptJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judge-cache.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load() error = nil, want an error for corrupt JSON content")
	}
}

func TestSamplesMissReturnsFalse(t *testing.T) {
	var c Cache
	if _, ok := c.Samples("missing-key"); ok {
		t.Error("Samples() ok = true, want false for a key never appended")
	}
}

func TestAppendThenSamplesRoundTrips(t *testing.T) {
	var c Cache
	s1 := Sample{Findings: []FindingRecord{{Name: "tone", Passed: true, Reason: "friendly"}}}
	s2 := Sample{Findings: []FindingRecord{{Name: "tone", Passed: false, Reason: "curt"}}}
	c.Append("key1", s1)
	c.Append("key1", s2)

	got, ok := c.Samples("key1")
	if !ok {
		t.Fatal("Samples() ok = false, want true after Append")
	}
	if len(got) != 2 {
		t.Fatalf("len(Samples()) = %d, want 2", len(got))
	}
	if got[0].Findings[0].Reason != "friendly" || got[1].Findings[0].Reason != "curt" {
		t.Errorf("Samples() = %+v, want [friendly, curt] in append order", got)
	}
}

func TestAppendToExistingKeyAddsToSameEntryRatherThanDuplicating(t *testing.T) {
	var c Cache
	c.Append("key1", Sample{Findings: []FindingRecord{{Name: "tone", Passed: true}}})
	c.Append("key1", Sample{Findings: []FindingRecord{{Name: "tone", Passed: false}}})
	if len(c.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1 (appending to an existing key must not create a second entry)", len(c.Entries))
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judge-cache.json")
	var c Cache
	c.Append("key1", Sample{Findings: []FindingRecord{{Name: "tone", Passed: true, Reason: "friendly"}}})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := loaded.Samples("key1")
	if !ok || len(got) != 1 || got[0].Findings[0].Reason != "friendly" {
		t.Errorf("Samples() after round-trip = %+v, %v, want one sample with Reason=friendly", got, ok)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".skillci", "judge-cache.json")
	var c Cache
	c.Append("key1", Sample{Findings: []FindingRecord{{Name: "tone", Passed: true}}})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
}

// TestSamplesReturnsIndependentCopy proves a caller mutating the slice
// (or an element) returned by Samples() cannot reach back into the
// Cache's own stored data — Samples() must hand back a copy, not an
// alias of the entry's internal []Sample.
func TestSamplesReturnsIndependentCopy(t *testing.T) {
	var c Cache
	c.Append("key1", Sample{Findings: []FindingRecord{{Name: "tone", Passed: true, Reason: "friendly"}}})

	got, ok := c.Samples("key1")
	if !ok {
		t.Fatal("Samples() ok = false, want true")
	}
	// Reassigning an existing element indexes straight into whatever
	// backing array got shares with the Cache — if Samples() ever starts
	// returning its internal slice by alias again, this line is what
	// would silently corrupt the cache's own stored data.
	got[0] = Sample{Findings: []FindingRecord{{Name: "tone", Passed: false, Reason: "mutated"}}}

	again, ok := c.Samples("key1")
	if !ok {
		t.Fatal("Samples() ok = false, want true")
	}
	if len(again) != 1 {
		t.Fatalf("Samples() after mutating a previous result = %d entries, want still 1 — appending to the returned slice must not grow the cache's own data", len(again))
	}
	if again[0].Findings[0].Reason != "friendly" {
		t.Errorf("Samples() Reason = %q, want %q — reassigning the returned slice's element must not mutate the cache's own data", again[0].Findings[0].Reason, "friendly")
	}
}

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
	if err := c.Save(filepath.Join(dir, "judge-cache.json")); err == nil {
		t.Error("Save() error = nil, want an error writing to a read-only directory")
	}
}

// TestAppendEvictsLeastRecentlyUsedEntryAtCap covers the LRU-not-FIFO
// eviction rule: appending to (i.e. using) an old entry must move it to
// the "most recently used" end, so it survives eviction pressure that a
// pure insertion-order cap would have dropped it under. This matters for
// `bisect`, which revisits the same commits (and so the same cache keys)
// across steps of its search.
func TestAppendEvictsLeastRecentlyUsedEntryAtCap(t *testing.T) {
	var c Cache
	// Fill to the cap with distinct keys.
	for i := range maxRetainedEntries {
		c.Append(Hash("m", "c", string(rune(i))), Sample{Findings: []FindingRecord{{Name: "x", Passed: true}}})
	}
	firstKey := Hash("m", "c", string(rune(0)))
	// Touch the first key again — this must mark it as recently used.
	c.Append(firstKey, Sample{Findings: []FindingRecord{{Name: "x", Passed: true}}})
	// Push one new key past the cap.
	newKey := Hash("m", "c", string(rune(maxRetainedEntries+1)))
	c.Append(newKey, Sample{Findings: []FindingRecord{{Name: "x", Passed: true}}})

	if len(c.Entries) != maxRetainedEntries {
		t.Fatalf("Entries = %d, want %d", len(c.Entries), maxRetainedEntries)
	}
	if _, ok := c.Samples(firstKey); !ok {
		t.Error("Samples() lost the recently-touched first key — LRU eviction should have protected it")
	}
	secondKey := Hash("m", "c", string(rune(1)))
	if _, ok := c.Samples(secondKey); ok {
		t.Error("Samples() found the second key, want it evicted — it was never touched after initial insertion, unlike firstKey")
	}
}
