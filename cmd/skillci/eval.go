package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/runner"
	"github.com/spf13/cobra"
)

func newEvalCmd() *cobra.Command {
	var model string
	cmd := &cobra.Command{
		Use:   "eval [path]",
		Short: "Run a skill's eval suite against a single model",
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

			cases, err := evalspec.LoadDir(filepath.Join(dir, "evals"))
			if err != nil {
				return err
			}
			if len(cases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no eval cases found in evals/")
				return nil
			}

			failed := 0
			for _, c := range cases {
				result, err := runner.RunCase(context.Background(), client, dir, model, c)
				if err != nil {
					return fmt.Errorf("running case %s: %w", c.Name, err)
				}
				status := "PASS"
				if !result.Passed {
					status = "FAIL"
					failed++
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s (%s)\n", status, c.Name, model)
				for _, f := range result.Failures {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", f)
				}
				if result.SnapshotDiff != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "  [SNAPSHOT CHANGED] %s\n", result.SnapshotDiff.Render)
				}
				if len(result.FuzzFindings) > 0 {
					flipped := 0
					for _, f := range result.FuzzFindings {
						if f.Flipped {
							flipped++
						}
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  [FUZZ] %d/%d mutations flipped trigger behavior\n", flipped, len(result.FuzzFindings))
					for _, f := range result.FuzzFindings {
						if f.Flipped {
							fmt.Fprintf(cmd.OutOrStdout(), "    %s: %q -> triggered=%v (want %v)\n", f.Mutation.Operator, f.Mutation.Prompt, f.Triggered, !f.Triggered)
						}
					}
				}
			}

			if failed > 0 {
				return fmt.Errorf("%d/%d case(s) failed", failed, len(cases))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&model, "model", "claude-sonnet-5", "model to evaluate against")
	return cmd
}
