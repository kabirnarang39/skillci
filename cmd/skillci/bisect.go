package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/bisect"
	"github.com/kabirnarang39/skillci/internal/bisectcache"
	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/gitutil"
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/kabirnarang39/skillci/internal/runner"
	"github.com/spf13/cobra"
)

func newBisectCmd() *cobra.Command {
	var path, model, good, bad string
	cmd := &cobra.Command{
		Use:   "bisect <case-name>",
		Short: "Binary-search a skill's git history to find which commit broke an eval case",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBisect(cmd.OutOrStdout(), args[0], path, model, good, bad)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "skill directory")
	cmd.Flags().StringVar(&model, "model", "claude-sonnet-5", "model to evaluate against")
	cmd.Flags().StringVar(&good, "good", "", "known-good commit SHA (auto-detected from history.json if omitted)")
	cmd.Flags().StringVar(&bad, "bad", "", "known-bad commit SHA (defaults to the most recent recorded run, or current HEAD, if omitted)")
	return cmd
}

// runBisect holds the bisect command's full logic as a plain function
// (rather than inline in newBisectCmd's RunE) so `regress --auto-bisect`
// can invoke it directly in-process, without shelling out to a second
// `skillci bisect` subprocess.
func runBisect(out io.Writer, caseName, path, model, good, bad string) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}
	client := anthropic.NewClient(apiKey)
	if base := os.Getenv("SKILLCI_BASE_URL"); base != "" {
		client = client.WithBaseURL(base)
	}

	cfg, err := config.Load(filepath.Join(path, ".skillci.yaml"))
	if err != nil {
		return err
	}
	cases, err := evalspec.LoadDir(filepath.Join(path, "evals"))
	if err != nil {
		return err
	}
	var target *evalspec.Case
	for i := range cases {
		if cases[i].Name == caseName {
			target = &cases[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("no eval case named %q found in evals/", caseName)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// gitutil.RepoRoot shells out to `git rev-parse --show-toplevel`,
	// which reports git's resolved (symlink-free) physical path. On
	// macOS (and similar), path is often under a symlinked prefix
	// (e.g. /var -> /private/var), so without resolving absPath the
	// same way, the filepath.Rel below silently computes a bogus
	// relative path — no error, just wrong — that filepath.Join
	// later "corrects" back to absPath itself instead of the
	// worktree, making every historical checkout read live content.
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}
	repoRoot, err := gitutil.RepoRoot(absPath)
	if err != nil {
		return fmt.Errorf("%s is not inside a git repository: %w", path, err)
	}
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return err
	}

	goodSHA, badSHA := good, bad
	historyPath := filepath.Join(path, ".skillci", "history.json")
	hist, err := history.Load(historyPath)
	if err != nil {
		return err
	}
	if goodSHA == "" {
		sha, ok := lastPassingSHA(hist, caseName, model)
		if !ok {
			return fmt.Errorf("no recorded passing run for case %q on model %q — supply --good explicitly", caseName, model)
		}
		goodSHA = sha
	}
	if badSHA == "" {
		sha, err := resolveBadSHA(hist, absPath)
		if err != nil {
			return err
		}
		badSHA = sha
	}

	changed, err := gitutil.LogPaths(absPath, goodSHA, badSHA, []string{"."})
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return fmt.Errorf("no commits touched %s between %s and %s — the regression isn't caused by a skill change", path, goodSHA, badSHA)
	}

	cachePath := filepath.Join(path, ".skillci", "bisect-cache.json")
	bcache, err := bisectcache.Load(cachePath)
	if err != nil {
		return err
	}

	test := func(sha string) (bool, error) {
		if passed, ok := bcache.Result(caseName, model, sha); ok {
			status := "fail"
			if passed {
				status = "pass"
			}
			fmt.Fprintf(out, "  %s — %s (cached)\n", shortSHA(sha), status)
			return passed, nil
		}
		worktreePath, cleanup, err := gitutil.WorktreeAdd(absPath, sha)
		if err != nil {
			return false, err
		}
		defer func() {
			if cerr := cleanup(); cerr != nil {
				fmt.Fprintf(out, "warning: failed to remove worktree at %s: %v\n", worktreePath, cerr)
			}
		}()
		result, err := runner.RunCase(context.Background(), client, filepath.Join(worktreePath, relPath), model, *target, cfg.Pricing, cfg.JudgeModel)
		if err != nil {
			return false, err
		}
		status := "fail"
		if result.Passed {
			status = "pass"
		}
		fmt.Fprintf(out, "  %s — %s\n", shortSHA(sha), status)
		bcache.Record(caseName, model, sha, result.Passed)
		if serr := bcache.Save(cachePath); serr != nil {
			fmt.Fprintf(out, "warning: failed to save bisect cache: %v\n", serr)
		}
		return result.Passed, nil
	}

	fmt.Fprintln(out, "verifying good/bad endpoints...")
	badPassed, err := test(badSHA)
	if err != nil {
		return err
	}
	if badPassed {
		return fmt.Errorf("case %q passes at --bad (%s) — not reproducible; check --bad or the case for non-determinism", caseName, badSHA)
	}

	goodPassed, err := test(goodSHA)
	if err != nil {
		return err
	}
	if !goodPassed {
		return fmt.Errorf("case %q does not pass at --good (%s) under model %s — the regression may be caused by the model itself, not a skill change", caseName, goodSHA, model)
	}

	goodInfo, err := gitutil.Show(absPath, goodSHA)
	if err != nil {
		return err
	}
	badInfo, err := gitutil.Show(absPath, badSHA)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "good: %s (%s) — passes\n", shortSHA(goodInfo.SHA), goodInfo.Date)
	fmt.Fprintf(out, "bad:  %s (%s) — fails\n", shortSHA(badInfo.SHA), badInfo.Date)

	var culprit string
	var additionalCulprits []string
	if len(changed) == 1 {
		culprit = changed[0]
	} else {
		// changed is the range (good, bad] from gitutil.LogPaths, so
		// its last element is always badSHA — already verified above.
		// Memoize so neither search algorithm below ever re-tests a
		// known endpoint.
		verified := map[string]bool{goodSHA: true, badSHA: false}
		cachedTest := func(sha string) (bool, error) {
			if v, ok := verified[sha]; ok {
				return v, nil
			}
			passed, err := test(sha)
			if err != nil {
				return false, err
			}
			verified[sha] = passed
			return passed, nil
		}

		hasMerges, err := gitutil.HasMergeCommits(absPath, goodSHA, badSHA, []string{"."})
		if err != nil {
			return err
		}
		if hasMerges {
			// Binary search assumes a strictly monotonic pass/fail
			// transition across a linear history — a merge commit
			// can violate that by interleaving commits from
			// different branches. Fall back to a full linear scan,
			// which makes no ordering assumption and can detect
			// more than one transition if the history genuinely
			// has disjoint culprits.
			fmt.Fprintf(out, "%d candidate commits — merge commit(s) detected in this range, scanning all of them linearly for correctness\n", len(changed))
			fmt.Fprintln(out, "bisecting...")
			culprit, additionalCulprits, err = bisect.SearchLinear(changed, cachedTest)
			if err != nil {
				return err
			}
		} else {
			fmt.Fprintf(out, "%d candidate commits, up to %d more API calls\n", len(changed), int(math.Ceil(math.Log2(float64(len(changed)+1)))))
			fmt.Fprintln(out, "bisecting...")
			culprit, err = bisect.Search(changed, cachedTest)
			if err != nil {
				return err
			}
		}
	}

	culpritInfo, err := gitutil.Show(absPath, culprit)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\nculprit: %s\n", culpritInfo.SHA)
	fmt.Fprintf(out, "author:  %s\n", culpritInfo.Author)
	fmt.Fprintf(out, "date:    %s\n", culpritInfo.Date)
	fmt.Fprintf(out, "message: %s\n\n", culpritInfo.Message)

	if len(additionalCulprits) > 0 {
		fmt.Fprintf(out, "warning: history in this range is non-linear — %d additional commit(s) also transition from passing to failing, so this regression may not have a single culprit:\n", len(additionalCulprits))
		for _, sha := range additionalCulprits {
			fmt.Fprintf(out, "  %s\n", shortSHA(sha))
		}
		fmt.Fprintln(out)
	}

	diff, err := gitutil.DiffFiles(absPath, culprit+"^", culprit, []string{"."})
	if err != nil {
		fmt.Fprintf(out, "(could not show a diff for this commit: %v)\n", err)
		return nil
	}
	fmt.Fprintln(out, diff)
	return nil
}

func lastPassingSHA(hist history.History, caseName, model string) (string, bool) {
	for i := len(hist.Runs) - 1; i >= 0; i-- {
		run := hist.Runs[i]
		if cr, ok := run.Result(caseName, model); ok && cr.Passed && run.CommitSHA != "" {
			return run.CommitSHA, true
		}
	}
	return "", false
}

func resolveBadSHA(hist history.History, absPath string) (string, error) {
	if run, ok := hist.LastRun(); ok && run.CommitSHA != "" {
		return run.CommitSHA, nil
	}
	return gitutil.RevParseHEAD(absPath)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
