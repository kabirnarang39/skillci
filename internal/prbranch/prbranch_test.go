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

// TestPushDoesNotCommitUnrelatedStagedChanges proves Push's commit is
// scoped to the paths it was given. Without that scoping, plain `git
// commit` commits the entire index — so any unrelated content a caller
// happened to have staged before calling Push (e.g. a developer's own
// in-progress work) would be swept into the auto-generated commit, pushed
// to a throwaway branch, and included in the pull request --open-pr opens.
func TestPushDoesNotCommitUnrelatedStagedChanges(t *testing.T) {
	dir := setupRepoWithRemote(t)

	secretFile := filepath.Join(dir, "unrelated-work-in-progress.txt")
	if err := os.WriteFile(secretFile, []byte("not meant for this PR"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "add", "unrelated-work-in-progress.txt")

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Push(dir, []string{newFile}, "add generated case", "origin", "skillci/generated-eval-test6"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	remoteFiles := gitOutput(t, dir, "ls-tree", "-r", "--name-only", "origin/skillci/generated-eval-test6")
	if strings.Contains(remoteFiles, "unrelated-work-in-progress.txt") {
		t.Errorf("pushed branch contains unrelated-work-in-progress.txt — Push must only commit the paths it was given, files = %q", remoteFiles)
	}

	// The unrelated file's staged status locally must also be untouched —
	// Push restores the original checkout but must not have consumed or
	// otherwise disturbed content outside the paths it owns.
	status := gitOutput(t, dir, "status", "--porcelain", "--", "unrelated-work-in-progress.txt")
	if status != "A  unrelated-work-in-progress.txt" {
		t.Errorf("unrelated-work-in-progress.txt status after Push() = %q, want still staged (A) and untouched", status)
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

// TestPushNotAGitRepoErrors covers CurrentBranch failing right at the
// start -- previously untested.
func TestPushNotAGitRepoErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Push(dir, []string{"x.txt"}, "msg", "origin", "some-branch"); err == nil {
		t.Error("Push() error = nil, want error outside a git repository")
	}
}

// TestPushErrorsWhenBranchAlreadyExists covers `git checkout -b` failing
// -- previously untested. Still must restore the original checkout
// afterward, same as every other failure path.
func TestPushErrorsWhenBranchAlreadyExists(t *testing.T) {
	dir := setupRepoWithRemote(t)
	origBranch := gitOutput(t, dir, "branch", "--show-current")
	runGitT(t, dir, "branch", "already-exists")

	newFile := filepath.Join(dir, "evals", "_generated", "new-case.yaml")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("name: new-case\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Push(dir, []string{newFile}, "add generated case", "origin", "already-exists"); err == nil {
		t.Fatal("Push() error = nil, want error creating a branch that already exists")
	}
	if got := gitOutput(t, dir, "branch", "--show-current"); got != origBranch {
		t.Errorf("branch after failed Push() = %q, want restored to original %q", got, origBranch)
	}
}

// TestPushErrorsWhenPathDoesNotExist covers `git add` failing on a
// nonexistent path -- previously untested.
func TestPushErrorsWhenPathDoesNotExist(t *testing.T) {
	dir := setupRepoWithRemote(t)

	if err := Push(dir, []string{filepath.Join(dir, "does-not-exist.yaml")}, "msg", "origin", "skillci/missing-path-test"); err == nil {
		t.Fatal("Push() error = nil, want error adding a nonexistent path")
	}
}
