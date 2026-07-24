// Package fuzzcache persists LLM-generated paraphrases (fuzz_llm) keyed
// by the exact prompt text they were generated from, so the same prompt
// never triggers a second paraphrase-generation API call — the model is
// asked once per unique prompt, ever, not once per run.
package fuzzcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// HashPrompt returns a stable cache key for prompt. Hashed rather than
// used as a raw map key so a very long prompt doesn't bloat the cache
// file's own key text, and so the on-disk format never needs to escape
// arbitrary prompt content into a key position.
func HashPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

type Entry struct {
	PromptHash  string   `json:"prompt_hash"`
	Paraphrases []string `json:"paraphrases"`
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

// Paraphrases returns the cached paraphrases for promptHash, if present.
func (c Cache) Paraphrases(promptHash string) ([]string, bool) {
	for _, e := range c.Entries {
		if e.PromptHash == promptHash {
			return e.Paraphrases, true
		}
	}
	return nil, false
}

// maxRetainedEntries bounds cache growth, matching internal/bisectcache
// and internal/history's identical retention convention for a
// git-committed artifact.
const maxRetainedEntries = 500

// Record stores promptHash's paraphrases, updating an existing entry in
// place if one already exists rather than appending a duplicate.
func (c *Cache) Record(promptHash string, paraphrases []string) {
	for i, e := range c.Entries {
		if e.PromptHash == promptHash {
			c.Entries[i].Paraphrases = paraphrases
			return
		}
	}
	c.Entries = append(c.Entries, Entry{PromptHash: promptHash, Paraphrases: paraphrases})
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
