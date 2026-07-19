package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, frontmatter, body string) {
	t.Helper()
	content := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLintSkillValid(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: my-skill\ndescription: Does a thing when asked.\n", "Body text.\n")

	issues, err := LintSkill(dir)
	if err != nil {
		t.Fatalf("LintSkill() error = %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("LintSkill() issues = %v, want none", issues)
	}
}

func TestLintSkillMissingDescription(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: my-skill\n", "Body text.\n")

	issues, err := LintSkill(dir)
	if err != nil {
		t.Fatalf("LintSkill() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Rule != "missing-description" {
		t.Errorf("LintSkill() issues = %v, want one missing-description issue", issues)
	}
}

func TestLintSkillMissingReferencedFile(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: my-skill\ndescription: Does a thing.\n", "See references/guide.md for details.\n")

	issues, err := LintSkill(dir)
	if err != nil {
		t.Fatalf("LintSkill() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "missing-referenced-file" {
			found = true
		}
	}
	if !found {
		t.Errorf("LintSkill() issues = %v, want a missing-referenced-file issue", issues)
	}
}

func TestLintSkillNoSkillFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LintSkill(dir)
	if err == nil {
		t.Error("LintSkill() error = nil, want error for missing SKILL.md")
	}
}
