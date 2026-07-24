// Package gitutil is a thin wrapper around shelling out to the system git
// binary for the plumbing internal/bisect needs: resolving commits, listing
// history filtered to specific paths, checking out historical content into
// ephemeral worktrees, and diffing/describing commits. It never touches the
// caller's actual working tree or HEAD.
package gitutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RevParseHEAD returns the current HEAD commit SHA for the git repository
// containing dir.
func RevParseHEAD(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// RepoRoot returns the absolute path to the top level of the git repository
// containing dir. dir may be any subdirectory of the repository.
func RepoRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CurrentBranch returns the current branch's short name, or the literal
// string "HEAD" when dir's checkout is in detached-HEAD state (there is no
// branch name to return). A read, like RevParseHEAD — it never mutates
// HEAD or the working tree.
func CurrentBranch(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// LogPaths returns commit SHAs in the range (fromSHA, toSHA] — excluding
// fromSHA, including toSHA — that touched any of paths, oldest first.
func LogPaths(dir, fromSHA, toSHA string, paths []string) ([]string, error) {
	args := []string{"log", "--reverse", "--format=%H", fromSHA + ".." + toSHA, "--"}
	args = append(args, paths...)
	out, err := runGit(dir, args...)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// HasMergeCommits reports whether any commit in the range (fromSHA, toSHA]
// touching paths has more than one parent — i.e., whether history in this
// range is non-linear. bisect.Search's binary search assumes a strictly
// monotonic pass/fail transition across a linear history; a merge commit
// can violate that assumption by interleaving commits from different
// branches, so the caller should fall back to bisect.SearchLinear when
// this returns true.
func HasMergeCommits(dir, fromSHA, toSHA string, paths []string) (bool, error) {
	args := []string{"log", "--min-parents=2", "--format=%H", fromSHA + ".." + toSHA, "--"}
	args = append(args, paths...)
	out, err := runGit(dir, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// WorktreeAdd creates a temporary, detached git worktree at sha and returns
// its root path plus a cleanup function that removes it. The caller must
// always call cleanup, even on error paths taken after WorktreeAdd itself
// has succeeded.
func WorktreeAdd(dir, sha string) (string, func() error, error) {
	worktreePath, err := os.MkdirTemp("", "skillci-bisect-*")
	if err != nil {
		return "", nil, err
	}
	// MkdirTemp already creates the directory, but `git worktree add`
	// requires the target path not exist yet — remove it and let git
	// recreate it at the checkout.
	if err := os.RemoveAll(worktreePath); err != nil {
		return "", nil, err
	}
	if _, err := runGit(dir, "worktree", "add", "--detach", worktreePath, sha); err != nil {
		return "", nil, err
	}
	cleanup := func() error {
		_, err := runGit(dir, "worktree", "remove", "--force", worktreePath)
		return err
	}
	return worktreePath, cleanup, nil
}

// DiffFiles returns the diff of paths between shaA and shaB.
func DiffFiles(dir, shaA, shaB string, paths []string) (string, error) {
	args := []string{"diff", shaA, shaB, "--"}
	args = append(args, paths...)
	return runGit(dir, args...)
}

// CommitInfo is a commit's identifying metadata, used for bisect's report.
type CommitInfo struct {
	SHA     string
	Author  string
	Date    string
	Message string
}

// Show returns sha's commit metadata (full SHA, "name <email>" author,
// YYYY-MM-DD author date, and subject line).
func Show(dir, sha string) (CommitInfo, error) {
	out, err := runGit(dir, "show", "-s", "--date=short", "--format=%H%x1f%an <%ae>%x1f%ad%x1f%s", sha)
	if err != nil {
		return CommitInfo{}, err
	}
	parts := strings.SplitN(strings.TrimSpace(out), "\x1f", 4)
	if len(parts) != 4 {
		return CommitInfo{}, fmt.Errorf("unexpected git show output: %q", out)
	}
	return CommitInfo{SHA: parts[0], Author: parts[1], Date: parts[2], Message: parts[3]}, nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
