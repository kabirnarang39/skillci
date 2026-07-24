package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")
	return dir
}

func commitFile(t *testing.T, dir, relPath, content, message string) string {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "add", relPath)
	runGitT(t, dir, "commit", "-q", "-m", message)
	sha, err := RevParseHEAD(dir)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRevParseHEAD(t *testing.T) {
	dir := initRepo(t)
	want := commitFile(t, dir, "a.txt", "hello", "initial")
	got, err := RevParseHEAD(dir)
	if err != nil {
		t.Fatalf("RevParseHEAD() error = %v", err)
	}
	if got != want {
		t.Errorf("RevParseHEAD() = %q, want %q", got, want)
	}
}

func TestRepoRootFromSubdirectory(t *testing.T) {
	dir := initRepo(t)
	commitFile(t, dir, "skill/SKILL.md", "content", "initial")
	skillDir := filepath.Join(dir, "skill")

	root, err := RepoRoot(skillDir)
	if err != nil {
		t.Fatalf("RepoRoot() error = %v", err)
	}
	// Resolve symlinks on both sides (e.g. macOS /tmp is a symlink to
	// /private/tmp, and git rev-parse --show-toplevel returns the resolved
	// path) so the comparison isn't thrown off by an unresolved prefix.
	wantRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != wantRoot {
		t.Errorf("RepoRoot() = %q, want %q", gotRoot, wantRoot)
	}
}

func TestRepoRootNotAGitRepoErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := RepoRoot(dir); err == nil {
		t.Error("RepoRoot() error = nil, want an error for a non-git directory")
	}
}

func TestLogPathsFiltersAndOrdersOldestFirst(t *testing.T) {
	dir := initRepo(t)
	sha1 := commitFile(t, dir, "a.txt", "v1", "a v1")
	commitFile(t, dir, "b.txt", "v1", "b v1 (unrelated)")
	sha3 := commitFile(t, dir, "a.txt", "v2", "a v2")
	sha4 := commitFile(t, dir, "a.txt", "v3", "a v3")

	got, err := LogPaths(dir, sha1, sha4, []string{"a.txt"})
	if err != nil {
		t.Fatalf("LogPaths() error = %v", err)
	}
	want := []string{sha3, sha4}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("LogPaths() = %v, want %v (oldest first, excluding sha1, excluding the unrelated b.txt commit)", got, want)
	}
}

func TestLogPathsEmptyWhenNoCommitsTouchPath(t *testing.T) {
	dir := initRepo(t)
	sha1 := commitFile(t, dir, "a.txt", "v1", "a v1")
	sha2 := commitFile(t, dir, "b.txt", "v1", "b only")

	got, err := LogPaths(dir, sha1, sha2, []string{"a.txt"})
	if err != nil {
		t.Fatalf("LogPaths() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LogPaths() = %v, want empty (no commit in range touched a.txt)", got)
	}
}

func TestWorktreeAddChecksOutHistoricalContentAndCleansUp(t *testing.T) {
	dir := initRepo(t)
	sha1 := commitFile(t, dir, "a.txt", "old", "v1")
	commitFile(t, dir, "a.txt", "new", "v2")

	worktreePath, cleanup, err := WorktreeAdd(dir, sha1)
	if err != nil {
		t.Fatalf("WorktreeAdd() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(worktreePath, "a.txt"))
	if err != nil {
		t.Fatalf("reading worktree file: %v", err)
	}
	if string(got) != "old" {
		t.Errorf("worktree a.txt = %q, want %q (the sha1 version, not current HEAD)", got, "old")
	}

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree directory still exists after cleanup: %v", err)
	}
}

func TestDiffFilesShowsChange(t *testing.T) {
	dir := initRepo(t)
	sha1 := commitFile(t, dir, "SKILL.md", "old content", "v1")
	sha2 := commitFile(t, dir, "SKILL.md", "new content", "v2")

	diff, err := DiffFiles(dir, sha1, sha2, []string{"."})
	if err != nil {
		t.Fatalf("DiffFiles() error = %v", err)
	}
	if !strings.Contains(diff, "-old content") || !strings.Contains(diff, "+new content") {
		t.Errorf("diff = %q, want lines removing %q and adding %q", diff, "old content", "new content")
	}
}

func TestShowReturnsCommitInfo(t *testing.T) {
	dir := initRepo(t)
	sha := commitFile(t, dir, "a.txt", "hello", "a descriptive message")

	info, err := Show(dir, sha)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if info.SHA != sha {
		t.Errorf("SHA = %q, want %q", info.SHA, sha)
	}
	if info.Message != "a descriptive message" {
		t.Errorf("Message = %q, want %q", info.Message, "a descriptive message")
	}
	if info.Author == "" {
		t.Error("Author is empty")
	}
	if info.Date == "" {
		t.Error("Date is empty")
	}
}
