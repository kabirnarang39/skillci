package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/badge"
	"github.com/kabirnarang/skillci/internal/config"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/kabirnarang/skillci/internal/regress"
	"github.com/spf13/cobra"
)

func newRegressCmd() *cobra.Command {
	var upload bool
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

			if upload {
				fmt.Fprintln(cmd.OutOrStdout(), "note: --upload wiring lands in a later task; results were not sent to the dashboard")
			}

			if report.ShouldFailCI(cfg.FailOn) {
				return fmt.Errorf("regression detected (fail_on=%s)", cfg.FailOn)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&upload, "upload", false, "upload results to the SkillCI dashboard")
	return cmd
}
