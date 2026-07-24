package prbranch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// setupRepoWithRemote creates a working repo with one commit on "main" and
// a bare "origin" remote it can actually push to, entirely on local disk —
// no network access.
func setupRepoWithRemote(t *testing.T) (dir string) {
	t.Helper()
	bareDir := t.TempDir()
	runGitT(t, bareDir, "init", "-q", "--bare")

	dir = t.TempDir()
	runGitT(t, dir, "init", "-q", "-b", "main")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-q", "-m", "initial")
	runGitT(t, dir, "remote", "add", "origin", bareDir)
	runGitT(t, dir, "push", "-q", "origin", "main")
	return dir
}

func TestPushCommitsFileAndPushesBranch(t *testing.T) {
	dir := setupRepoWithRemote(t)

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Push(dir, []string{newFile}, "add generated case", "origin", "skillci/generated-eval-test"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	// The remote must actually have the new branch with the new commit.
	branches := gitOutput(t, dir, "ls-remote", "--heads", "origin")
	if !strings.Contains(branches, "refs/heads/skillci/generated-eval-test") {
		t.Errorf("remote branches = %q, want skillci/generated-eval-test to be present", branches)
	}

	remoteLog := gitOutput(t, dir, "log", "origin/skillci/generated-eval-test", "-1", "--format=%s")
	if remoteLog != "add generated case" {
		t.Errorf("remote branch tip commit message = %q, want %q", remoteLog, "add generated case")
	}
}

func TestPushRestoresOriginalHEADAfterSuccess(t *testing.T) {
	dir := setupRepoWithRemote(t)
	origSHA := gitOutput(t, dir, "rev-parse", "HEAD")
	origBranch := gitOutput(t, dir, "branch", "--show-current")

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Push(dir, []string{newFile}, "add generated case", "origin", "skillci/generated-eval-test2"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != origSHA {
		t.Errorf("local HEAD after Push() = %q, want restored to original %q", got, origSHA)
	}
	if got := gitOutput(t, dir, "branch", "--show-current"); got != origBranch {
		t.Errorf("local branch after Push() = %q, want restored to original %q", got, origBranch)
	}
}

func TestPushRestoresOriginalHEADEvenWhenPushFails(t *testing.T) {
	dir := setupRepoWithRemote(t)
	origSHA := gitOutput(t, dir, "rev-parse", "HEAD")

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A remote that doesn't exist forces the push step to fail after the
	// branch has already been created and committed locally.
	err := Push(dir, []string{newFile}, "add generated case", "no-such-remote", "skillci/generated-eval-test3")
	if err == nil {
		t.Fatal("Push() error = nil, want an error pushing to a nonexistent remote")
	}

	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != origSHA {
		t.Errorf("local HEAD after failed Push() = %q, want restored to original %q — a failed attempt must not strand the checkout", got, origSHA)
	}
}

func TestPushRestoresDetachedHEADRatherThanABranch(t *testing.T) {
	dir := setupRepoWithRemote(t)
	origSHA := gitOutput(t, dir, "rev-parse", "HEAD")
	runGitT(t, dir, "checkout", "-q", origSHA) // detach, same as many CI checkouts

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Push(dir, []string{newFile}, "add generated case", "origin", "skillci/generated-eval-test5"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != origSHA {
		t.Errorf("local HEAD after Push() = %q, want restored to original detached SHA %q", got, origSHA)
	}
	if got := gitOutput(t, dir, "branch", "--show-current"); got != "" {
		t.Errorf("branch after Push() = %q, want empty (still detached, matching the pre-Push state)", got)
	}
}

func TestPushCreatesUniqueBranchFromCurrentHEAD(t *testing.T) {
	dir := setupRepoWithRemote(t)

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Push(dir, []string{newFile}, "add generated case", "origin", "skillci/generated-eval-test4"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	base := gitOutput(t, dir, "merge-base", "main", "origin/skillci/generated-eval-test4")
	mainSHA := gitOutput(t, dir, "rev-parse", "main")
	if base != mainSHA {
		t.Errorf("merge-base(main, pushed branch) = %q, want %q (pushed branch must be created from main)", base, mainSHA)
	}
}
