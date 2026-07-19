package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFile(t *testing.T) {
	h, err := Load(filepath.Join(t.TempDir(), ".skillci", "history.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(h.Runs) != 0 {
		t.Errorf("Runs = %v, want empty", h.Runs)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skillci", "history.json")
	h := History{}
	h.Append(Run{
		Timestamp: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		CommitSHA: "abc123",
		Cases: []CaseResult{
			{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
		},
	})

	if err := h.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Runs) != 1 || loaded.Runs[0].CommitSHA != "abc123" {
		t.Errorf("loaded = %+v, want one run with commit abc123", loaded)
	}
}

func TestLastRun(t *testing.T) {
	h := History{}
	h.Append(Run{CommitSHA: "first"})
	h.Append(Run{CommitSHA: "second"})

	last, ok := h.LastRun()
	if !ok || last.CommitSHA != "second" {
		t.Errorf("LastRun() = %+v, %v, want second run", last, ok)
	}
}

func TestLastRunEmpty(t *testing.T) {
	h := History{}
	_, ok := h.LastRun()
	if ok {
		t.Error("LastRun() ok = true, want false for empty history")
	}
}

func TestRunResult(t *testing.T) {
	run := Run{Cases: []CaseResult{
		{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
		{Name: "case-a", Model: "claude-opus-4-8", Passed: false},
	}}
	r, ok := run.Result("case-a", "claude-opus-4-8")
	if !ok || r.Passed {
		t.Errorf("Result() = %+v, %v, want passed=false", r, ok)
	}
	_, ok = run.Result("case-a", "claude-haiku-4-5")
	if ok {
		t.Error("Result() ok = true, want false for model not in run")
	}
}
