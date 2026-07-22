package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
)

// GoldenPath returns the path to the accepted golden-baseline file for a
// case+model, under skillDir/evals/. Per-model because different models
// legitimately phrase correct responses differently.
func GoldenPath(skillDir, caseName, model string) string {
	return filepath.Join(skillDir, "evals", caseName+"."+model+".golden.txt")
}

// PendingPath returns the path to a not-yet-accepted snapshot change,
// reusing the existing evals/_generated/ directory (same directory the
// self-growing eval loop already writes proposed eval cases into).
func PendingPath(skillDir, caseName, model string) string {
	return filepath.Join(skillDir, "evals", "_generated", caseName+"."+model+".golden.txt")
}

// Load reads the stored golden text for a case+model. A missing file is
// not an error — it returns ok=false, matching the tool's existing
// forgiving posture toward missing state (see internal/history.Load).
func Load(skillDir, caseName, model string) (text string, ok bool, err error) {
	return readIfExists(GoldenPath(skillDir, caseName, model))
}

// Save writes golden text for a case+model, creating the evals/ directory
// if needed.
func Save(skillDir, caseName, model, text string) error {
	return writeCreatingParents(GoldenPath(skillDir, caseName, model), text)
}

// LoadPending reads a not-yet-accepted snapshot change, if one exists.
func LoadPending(skillDir, caseName, model string) (text string, ok bool, err error) {
	return readIfExists(PendingPath(skillDir, caseName, model))
}

// SavePending writes a not-yet-accepted snapshot change.
func SavePending(skillDir, caseName, model, text string) error {
	return writeCreatingParents(PendingPath(skillDir, caseName, model), text)
}

// PromotePending moves a pending snapshot change into the accepted golden
// file, overwriting any existing golden baseline, and removes the pending
// file. Errors if no pending file exists for this case+model.
func PromotePending(skillDir, caseName, model string) error {
	text, ok, err := LoadPending(skillDir, caseName, model)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no pending snapshot change for %s (%s)", caseName, model)
	}
	if err := Save(skillDir, caseName, model, text); err != nil {
		return err
	}
	return os.Remove(PendingPath(skillDir, caseName, model))
}

func readIfExists(path string) (text string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func writeCreatingParents(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
