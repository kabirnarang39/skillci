package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingGolden(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := Load(dir, "my-case", "claude-sonnet-5")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if ok {
		t.Error("ok = true, want false for missing golden file")
	}
}

func TestSaveAndLoadGoldenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "my-case", "claude-sonnet-5", "the golden response"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	text, ok, err := Load(dir, "my-case", "claude-sonnet-5")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true after Save")
	}
	if text != "the golden response" {
		t.Errorf("text = %q, want %q", text, "the golden response")
	}
}

func TestGoldenPathIsPerCaseAndModel(t *testing.T) {
	dir := t.TempDir()
	p1 := GoldenPath(dir, "my-case", "claude-sonnet-5")
	p2 := GoldenPath(dir, "my-case", "claude-opus-4-8")
	if p1 == p2 {
		t.Error("golden paths for different models must differ")
	}
	if filepath.Dir(p1) != filepath.Join(dir, "evals") {
		t.Errorf("golden file dir = %q, want %q", filepath.Dir(p1), filepath.Join(dir, "evals"))
	}
}

func TestSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "nested", "skill")
	if err := Save(skillDir, "my-case", "claude-sonnet-5", "text"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(GoldenPath(skillDir, "my-case", "claude-sonnet-5")); err != nil {
		t.Errorf("golden file not created: %v", err)
	}
}

func TestSavePendingAndLoadPendingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SavePending(dir, "my-case", "claude-sonnet-5", "a new response"); err != nil {
		t.Fatalf("SavePending() error = %v", err)
	}
	text, ok, err := LoadPending(dir, "my-case", "claude-sonnet-5")
	if err != nil {
		t.Fatalf("LoadPending() error = %v", err)
	}
	if !ok || text != "a new response" {
		t.Errorf("LoadPending() = %q, %v, want %q, true", text, ok, "a new response")
	}
}

func TestPendingPathLivesUnderGeneratedDir(t *testing.T) {
	dir := t.TempDir()
	p := PendingPath(dir, "my-case", "claude-sonnet-5")
	if filepath.Dir(p) != filepath.Join(dir, "evals", "_generated") {
		t.Errorf("pending path dir = %q, want evals/_generated", filepath.Dir(p))
	}
}

func TestPromotePendingMovesFileAndOverwritesGolden(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "my-case", "claude-sonnet-5", "old golden"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := SavePending(dir, "my-case", "claude-sonnet-5", "new golden"); err != nil {
		t.Fatalf("SavePending() error = %v", err)
	}
	if err := PromotePending(dir, "my-case", "claude-sonnet-5"); err != nil {
		t.Fatalf("PromotePending() error = %v", err)
	}
	text, ok, err := Load(dir, "my-case", "claude-sonnet-5")
	if err != nil || !ok {
		t.Fatalf("Load() after promote = %q, %v, %v", text, ok, err)
	}
	if text != "new golden" {
		t.Errorf("golden after promote = %q, want %q", text, "new golden")
	}
	if _, ok, _ := LoadPending(dir, "my-case", "claude-sonnet-5"); ok {
		t.Error("pending file should be removed after promote")
	}
}

func TestPromotePendingErrorsWhenNoPendingExists(t *testing.T) {
	dir := t.TempDir()
	if err := PromotePending(dir, "no-such-case", "claude-sonnet-5"); err == nil {
		t.Error("PromotePending() error = nil, want error when no pending file exists")
	}
}
