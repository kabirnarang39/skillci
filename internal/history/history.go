package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type CaseResult struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	Passed bool   `json:"passed"`
}

type Run struct {
	Timestamp time.Time    `json:"timestamp"`
	CommitSHA string       `json:"commit_sha"`
	Cases     []CaseResult `json:"cases"`
}

// Result returns the recorded result for caseName+model in this run, if present.
func (r Run) Result(caseName, model string) (CaseResult, bool) {
	for _, c := range r.Cases {
		if c.Name == caseName && c.Model == model {
			return c, true
		}
	}
	return CaseResult{}, false
}

type History struct {
	Runs []Run `json:"runs"`
}

func Load(path string) (History, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return History{}, nil
	}
	if err != nil {
		return History{}, err
	}
	var h History
	if err := json.Unmarshal(data, &h); err != nil {
		return History{}, err
	}
	return h, nil
}

// maxRetainedRuns bounds how many historical runs Append keeps, so
// .skillci/history.json — a git-committed artifact — doesn't grow
// unbounded across a project's lifetime. Matches the same 200-run window
// internal/dashboard/store.go's SkillHistory query already treats as
// "recent history." Only the oldest runs are dropped; bisect's auto
// good/bad detection still works over whatever window is retained, and a
// regression older than that needs an explicit --good override, which
// the CLI already supports.
const maxRetainedRuns = 200

func (h *History) Append(r Run) {
	h.Runs = append(h.Runs, r)
	if len(h.Runs) > maxRetainedRuns {
		h.Runs = h.Runs[len(h.Runs)-maxRetainedRuns:]
	}
}

func (h History) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (h History) LastRun() (Run, bool) {
	if len(h.Runs) == 0 {
		return Run{}, false
	}
	return h.Runs[len(h.Runs)-1], true
}
