package history

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestLoadCorruptJSONReturnsError covers a history.json that exists but
// isn't valid JSON — a real, plausible scenario for a git-committed file:
// an unresolved merge conflict left in it, or a write truncated by a
// killed process. Previously untested; only the "file doesn't exist" and
// "valid JSON" branches had coverage.
func TestLoadCorruptJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("<<<<<<< HEAD\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load() error = nil, want an error for corrupt JSON content")
	}
}

// TestSaveToUnwritableDirectoryReturnsError covers os.WriteFile itself
// failing — previously untested; every existing Save test wrote to a
// normal writable temp dir. Skipped on Windows, where chmod-based
// read-only directories don't block writes the same way.
func TestSaveToUnwritableDirectoryReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions don't block writes the same way on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	h := History{}
	if err := h.Save(filepath.Join(dir, "history.json")); err == nil {
		t.Error("Save() error = nil, want an error writing to a read-only directory")
	}
}

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
	}, 0)

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
	h.Append(Run{CommitSHA: "first"}, 0)
	h.Append(Run{CommitSHA: "second"}, 0)

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

func TestAppendCapsRetainedRuns(t *testing.T) {
	h := History{}
	// One more than the cap, each uniquely identifiable by CommitSHA.
	for i := 0; i < DefaultMaxRetainedRuns+1; i++ {
		h.Append(Run{CommitSHA: string(rune('A'+i%26)) + string(rune(i))}, 0)
	}
	if len(h.Runs) != DefaultMaxRetainedRuns {
		t.Fatalf("len(Runs) = %d, want %d (the cap)", len(h.Runs), DefaultMaxRetainedRuns)
	}
}

func TestAppendCapKeepsNewestRunsNotOldest(t *testing.T) {
	h := History{}
	for i := 0; i < DefaultMaxRetainedRuns+5; i++ {
		h.Append(Run{CommitSHA: "run-" + string(rune('0'+i%10))}, 0)
	}
	last, ok := h.LastRun()
	if !ok {
		t.Fatal("LastRun() ok = false, want true")
	}
	// The very last Append call (index DefaultMaxRetainedRuns+4) must survive the
	// cap — proving the retained window is the newest runs, not just an
	// arbitrary truncation to the front.
	wantSuffix := "run-" + string(rune('0'+(DefaultMaxRetainedRuns+4)%10))
	if last.CommitSHA != wantSuffix {
		t.Errorf("LastRun().CommitSHA = %q, want %q — the cap must drop the OLDEST runs, keeping the most recent one intact", last.CommitSHA, wantSuffix)
	}
	if len(h.Runs) != DefaultMaxRetainedRuns {
		t.Errorf("len(Runs) = %d, want %d", len(h.Runs), DefaultMaxRetainedRuns)
	}
}

// TestAppendCustomMaxRunsOverridesDefault proves maxRuns is a real,
// honored override — not just accepted and ignored — the mechanism a
// team with a fast CI cadence depends on to keep enough history to span
// a wall-clock retention window (e.g. the EU AI Act's 6-month minimum)
// that the 200-run default might not reach.
func TestAppendCustomMaxRunsOverridesDefault(t *testing.T) {
	h := History{}
	for i := 0; i < 5; i++ {
		h.Append(Run{CommitSHA: "run-" + string(rune('0'+i))}, 3)
	}
	if len(h.Runs) != 3 {
		t.Fatalf("len(Runs) = %d, want 3 (the custom cap, not the 200 default)", len(h.Runs))
	}
	last, _ := h.LastRun()
	if last.CommitSHA != "run-4" {
		t.Errorf("LastRun().CommitSHA = %q, want run-4 — a custom cap must still keep the newest runs, not the oldest", last.CommitSHA)
	}
}

// TestAppendZeroMaxRunsFallsBackToDefault proves 0 means "use
// DefaultMaxRetainedRuns," not "cap at zero runs" — the value every
// existing .skillci.yaml (with no history_retention_runs set) implicitly
// passes, so this must never silently discard all history.
func TestAppendZeroMaxRunsFallsBackToDefault(t *testing.T) {
	h := History{}
	h.Append(Run{CommitSHA: "only-run"}, 0)
	if len(h.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1 — maxRuns=0 must fall back to the default cap, not truncate to zero", len(h.Runs))
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
