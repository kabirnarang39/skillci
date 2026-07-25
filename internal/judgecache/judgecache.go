// Package judgecache persists LLM-as-judge verdict samples keyed by
// judge model, criteria set, and the exact response text they were
// generated from — so a repeat judge call against unchanged inputs (a CI
// retry, a `bisect` step re-visiting an already-tested commit, a
// deterministic/greedy-decoded case model reproducing the same response)
// never re-pays for an API call it already has an answer for.
//
// Unlike internal/fuzzcache (a fixed prompt library with a high expected
// reuse rate), judge inputs are a fresh model response on most runs, so
// this cache's hit rate is lower and its entries are less likely to be
// reused — eviction here is LRU (least-recently-*used*), not
// least-recently-inserted, so a key that keeps getting touched (e.g. by
// a `bisect` run revisiting the same commit) survives eviction pressure
// that a pure insertion-order cap would drop it under.
package judgecache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Hash returns a stable cache key for the given judge model, criteria
// set, and response text. parts are joined with a separator byte that
// can't appear in any of skillci's own text (judge model names, criteria
// names/text, and model responses are all plain text) so no two distinct
// (model, criteria, response) combinations can collide onto the same
// joined string before hashing.
func Hash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// FindingRecord is one criterion's verdict from a single judge call —
// the cached equivalent of runner.JudgeFinding, kept as its own type so
// this package has no dependency on internal/runner.
type FindingRecord struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

// Sample is every criterion's verdict from one judge call (one call
// covers every criterion given to it at once — see internal/runner's
// judge_mode grouping for what goes into a single call).
type Sample struct {
	Findings []FindingRecord `json:"findings"`
}

// Entry is every sample recorded so far for one cache key.
type Entry struct {
	Key     string   `json:"key"`
	Samples []Sample `json:"samples"`
}

type Cache struct {
	Entries []Entry `json:"entries"`
}

// Load reads a judge-cache.json file at path. A missing file is not an
// error — it returns an empty Cache, same as internal/fuzzcache. Corrupt
// JSON IS returned as an error here (unlike a missing file); callers in
// internal/runner treat that error as "cache unavailable this run," not
// a case failure — deliberately more lenient at the call site than
// fuzzcache's caller, since a git-merge-conflict-truncated cache file
// should degrade judge caching to "always call live," not break `regress`.
func Load(path string) (Cache, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Cache{}, nil
	}
	if err != nil {
		return Cache{}, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, err
	}
	return c, nil
}

// Samples returns every verdict sample recorded so far for key, in the
// order they were appended. The returned slice is a copy — a caller
// appending to it, or reassigning one of its elements, cannot reach back
// into the Cache's own stored data (a shallow copy of the []Sample slice
// header is enough for that guarantee: Sample/FindingRecord hold nothing
// but plain data, so there's no nested pointer left for a deep copy to
// protect).
func (c Cache) Samples(key string) ([]Sample, bool) {
	for _, e := range c.Entries {
		if e.Key == key {
			samples := make([]Sample, len(e.Samples))
			copy(samples, e.Samples)
			return samples, true
		}
	}
	return nil, false
}

// maxRetainedEntries bounds cache growth, matching internal/fuzzcache,
// internal/bisectcache, and internal/history's identical retention
// convention for a git-committed artifact.
const maxRetainedEntries = 500

// Append records one more sample for key, creating the entry if it
// doesn't exist yet. Using (appending to, or creating) an entry always
// moves it to the most-recently-used end of Entries — when the cache is
// over its cap, the least-recently-used entry (the front of the slice)
// is evicted, not the oldest-inserted one, so a key that keeps getting
// revisited (e.g. by `bisect` re-testing the same commit across steps of
// its search) survives eviction pressure a FIFO cap would drop it under.
func (c *Cache) Append(key string, sample Sample) {
	for i, e := range c.Entries {
		if e.Key == key {
			e.Samples = append(e.Samples, sample)
			c.Entries = append(c.Entries[:i], c.Entries[i+1:]...)
			c.Entries = append(c.Entries, e)
			return
		}
	}
	c.Entries = append(c.Entries, Entry{Key: key, Samples: []Sample{sample}})
	if len(c.Entries) > maxRetainedEntries {
		c.Entries = c.Entries[len(c.Entries)-maxRetainedEntries:]
	}
}

func (c Cache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
