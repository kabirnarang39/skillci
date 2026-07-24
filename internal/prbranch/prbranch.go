// Package prbranch commits specific files onto a new branch and pushes it
// to a remote, restoring the caller's original branch/commit locally
// afterward. It is the one place in skillci that mutates the caller's
// working tree/HEAD — kept deliberately separate from internal/gitutil,
// which documents itself as never doing that.
package prbranch

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/kabirnarang39/skillci/internal/gitutil"
)

// Push creates branchName from the current HEAD, commits paths onto it
// with message, and pushes it to remote. Regardless of success or failure
// partway through, it restores dir's HEAD to whatever it pointed to before
// Push was called — the original branch, if there was one, or the exact
// commit SHA if the checkout was already detached — so a failed
// --open-pr attempt never leaves the caller's checkout stranded on a
// throwaway branch.
func Push(dir string, paths []string, message, remote, branchName string) (err error) {
	origRef, err := gitutil.CurrentBranch(dir)
	if err != nil {
		return err
	}
	restoreTarget := origRef
	if restoreTarget == "HEAD" {
		sha, shaErr := gitutil.RevParseHEAD(dir)
		if shaErr != nil {
			return shaErr
		}
		restoreTarget = sha
	}
	defer func() {
		if _, restoreErr := runGit(dir, "checkout", restoreTarget); restoreErr != nil && err == nil {
			err = fmt.Errorf("restoring original checkout after push: %w", restoreErr)
		}
	}()

	if _, err := runGit(dir, "checkout", "-b", branchName); err != nil {
		return err
	}

	args := append([]string{"add"}, paths...)
	if _, err := runGit(dir, args...); err != nil {
		return err
	}
	if _, err := runGit(dir, "commit", "-m", message); err != nil {
		return err
	}
	if _, err := runGit(dir, "push", remote, branchName); err != nil {
		return err
	}
	return nil
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
