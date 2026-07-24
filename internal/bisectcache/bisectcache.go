// Package bisectcache persists (case, model, commit SHA) → passed/failed
// verdicts across separate `skillci bisect` invocations, so a repeat
// bisect run — on the same case, or on a different case whose range
// overlaps a commit already tested — never re-checks out a worktree and
// re-runs a case against a commit it already has a verified answer for.
package bisectcache

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Entry struct {
	CaseName string `json:"case_name"`
	Model    string `json:"model"`
	SHA      string `json:"sha"`
	Passed   bool   `json:"passed"`
}

type Cache struct {
	Entries []Entry `json:"entries"`
}

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

// Result returns the cached verdict for caseName+model+sha, if present.
func (c Cache) Result(caseName, model, sha string) (passed bool, ok bool) {
	for _, e := range c.Entries {
		if e.CaseName == caseName && e.Model == model && e.SHA == sha {
			return e.Passed, true
		}
	}
	return false, false
}

// maxRetainedEntries bounds how many entries Record keeps, for the same
// reason internal/history's maxRetainedRuns does — this is a
// git-committed artifact and shouldn't grow unbounded. A verified
// endpoint older than this window is simply re-tested, same as it would
// be on a machine that never had a cache at all.
const maxRetainedEntries = 500

// Record stores caseName+model+sha's verdict, updating an existing entry
// in place if one already exists rather than appending a duplicate.
func (c *Cache) Record(caseName, model, sha string, passed bool) {
	for i, e := range c.Entries {
		if e.CaseName == caseName && e.Model == model && e.SHA == sha {
			c.Entries[i].Passed = passed
			return
		}
	}
	c.Entries = append(c.Entries, Entry{CaseName: caseName, Model: model, SHA: sha, Passed: passed})
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
