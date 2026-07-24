package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/badge"
	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/kabirnarang39/skillci/internal/regress"
	"github.com/kabirnarang39/skillci/internal/upload"
	"github.com/spf13/cobra"
)

func newRegressCmd() *cobra.Command {
	var uploadFlag bool
	cmd := &cobra.Command{
		Use:   "regress [path]",
		Short: "Run the eval suite across the configured model matrix and fail CI on new regressions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY is not set")
			}
			client := anthropic.NewClient(apiKey)
			if base := os.Getenv("SKILLCI_BASE_URL"); base != "" {
				client = client.WithBaseURL(base)
			}

			cfg, err := config.Load(filepath.Join(dir, ".skillci.yaml"))
			if err != nil {
				return err
			}
			cases, err := evalspec.LoadDir(filepath.Join(dir, "evals"))
			if err != nil {
				return err
			}
			if len(cases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no eval cases found in evals/")
				return nil
			}

			historyPath := filepath.Join(dir, ".skillci", "history.json")
			hist, err := history.Load(historyPath)
			if err != nil {
				return err
			}

			report, newRun, err := regress.RunMatrix(context.Background(), client, dir, cfg, cases, hist)
			if err != nil {
				return err
			}

			for _, o := range report.Outcomes {
				status := "PASS"
				switch {
				case o.IsNewRegression:
					status = "REGRESSED"
				case !o.Result.Passed:
					status = "FAIL"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s (%s)\n", status, o.Case.Name, o.Model)
				for _, f := range o.Result.Failures {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", f)
				}
				if o.Result.SnapshotDiff != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "  [SNAPSHOT CHANGED] %s\n", o.Result.SnapshotDiff.Render)
				}
				if len(o.Result.FuzzFindings) > 0 {
					flipped := 0
					for _, f := range o.Result.FuzzFindings {
						if f.Flipped {
							flipped++
						}
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  [FUZZ] %d/%d mutations flipped trigger behavior\n", flipped, len(o.Result.FuzzFindings))
					for _, f := range o.Result.FuzzFindings {
						if f.Flipped {
							fmt.Fprintf(cmd.OutOrStdout(), "    %s: %q -> triggered=%v (want %v)\n", f.Mutation.Operator, f.Mutation.Prompt, f.Triggered, !f.Triggered)
						}
					}
				}
			}

			if len(report.GeneratedCases) > 0 {
				paths, err := regress.WriteGeneratedCases(dir, report.GeneratedCases)
				if err != nil {
					return err
				}
				for _, p := range paths {
					fmt.Fprintf(cmd.OutOrStdout(), "proposed new eval case: %s (run `skillci accept <name>` to keep it)\n", p)
				}
			} else {
				// still ensure the directory exists so tooling/tests can rely on its presence
				if err := os.MkdirAll(filepath.Join(dir, "evals", "_generated"), 0o755); err != nil {
					return err
				}
			}

			newRun.Timestamp = time.Now()
			newRun.CommitSHA = os.Getenv("GITHUB_SHA")

			hist.Append(newRun)
			if err := hist.Save(historyPath); err != nil {
				return err
			}

			state := badge.StateFromRun(newRun)
			if err := os.WriteFile(filepath.Join(dir, ".skillci", "badge.svg"), []byte(badge.Render(state)), 0o644); err != nil {
				return err
			}

			if uploadFlag {
				dashboardURL := os.Getenv("SKILLCI_DASHBOARD_URL")
				token := os.Getenv("SKILLCI_INGEST_TOKEN")
				if dashboardURL == "" || token == "" {
					return fmt.Errorf("--upload requires SKILLCI_DASHBOARD_URL and SKILLCI_INGEST_TOKEN")
				}
				owner, repoName := parseOwnerRepo(os.Getenv("GITHUB_REPOSITORY"))
				commitSHA := os.Getenv("GITHUB_SHA")

				// dir defaults to "." when running from inside a skill's own
				// directory (the common usage pattern); filepath.Base(".")
				// would upload under the bogus skill name ".", so resolve to
				// an absolute path first.
				absDir, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				skillName := filepath.Base(absDir)

				// Aggregate per model before uploading: the dashboard's
				// Leaderboard query keeps only the latest-inserted row per
				// (skill, model), so one upload.Send per eval case would let
				// whichever case is inserted last silently decide the
				// leaderboard's pass/fail state. A model only counts as
				// passing here if every case for it passed.
				passedByModel := make(map[string]bool, len(cfg.Models))
				for _, c := range newRun.Cases {
					if passed, ok := passedByModel[c.Model]; !ok {
						passedByModel[c.Model] = c.Passed
					} else {
						passedByModel[c.Model] = passed && c.Passed
					}
				}
				for _, model := range cfg.Models {
					passed, ok := passedByModel[model]
					if !ok {
						continue
					}
					err := upload.Send(context.Background(), dashboardURL, token, upload.Result{
						RepoOwner: owner, Repo: repoName, Skill: skillName,
						CommitSHA: commitSHA, Model: model, Passed: passed,
					})
					if err != nil {
						// per design §8: a dashboard hiccup must never break CI
						fmt.Fprintf(cmd.OutOrStdout(), "warning: dashboard upload failed: %v\n", err)
					}
				}
			}

			if report.ShouldFailCI(cfg.FailOn) {
				return fmt.Errorf("regression detected (fail_on=%s)", cfg.FailOn)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&uploadFlag, "upload", false, "upload results to the SkillCI dashboard")
	return cmd
}

func parseOwnerRepo(githubRepository string) (owner, repo string) {
	parts := strings.SplitN(githubRepository, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
