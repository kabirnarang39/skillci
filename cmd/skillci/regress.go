package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/badge"
	"github.com/kabirnarang/skillci/internal/config"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/kabirnarang/skillci/internal/regress"
	"github.com/kabirnarang/skillci/internal/upload"
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
				for _, c := range newRun.Cases {
					err := upload.Send(context.Background(), dashboardURL, token, upload.Result{
						RepoOwner: owner, Repo: repoName, Skill: filepath.Base(dir),
						CommitSHA: commitSHA, Model: c.Model, Passed: c.Passed,
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
