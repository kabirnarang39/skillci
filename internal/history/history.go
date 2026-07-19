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

func (h *History) Append(r Run) {
	h.Runs = append(h.Runs, r)
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
