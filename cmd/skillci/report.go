package main

import (
	"fmt"
	"path/filepath"

	"github.com/kabirnarang39/skillci/internal/compliance"
	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/spf13/cobra"
)

func newReportCmd() *cobra.Command {
	var complianceFramework string
	cmd := &cobra.Command{
		Use:   "report [path]",
		Short: "Generate an evidence report from a skill's eval cases and run history",
		Long: `Generate a Markdown evidence report from a skill's eval cases
(evals/*.yaml) and run history (.skillci/history.json), no API calls.

--compliance maps that evidence onto a governance framework's own
documentation/testing requirements: "nist-ai-rmf" (NIST AI RMF's Measure
function) or "eu-ai-act" (EU AI Act Articles 11/12/19/26).

This is evidence, not certification — skillci cannot certify a skill
compliant with anything. It reports what testing/logging artifacts
already exist, for an auditor or governance reviewer to inspect.`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if complianceFramework != "nist-ai-rmf" && complianceFramework != "eu-ai-act" {
				return fmt.Errorf(`--compliance must be "nist-ai-rmf" or "eu-ai-act", got %q`, complianceFramework)
			}

			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			cases, err := evalspec.LoadDir(filepath.Join(dir, "evals"))
			if err != nil {
				return err
			}
			hist, err := history.Load(filepath.Join(dir, ".skillci", "history.json"))
			if err != nil {
				return err
			}
			cfg, err := config.Load(filepath.Join(dir, ".skillci.yaml"))
			if err != nil {
				return err
			}

			ev := compliance.Gather(cases, hist)

			var report string
			switch complianceFramework {
			case "nist-ai-rmf":
				report = compliance.RenderNISTAIRMF(ev, dir)
			case "eu-ai-act":
				report = compliance.RenderEUAIAct(ev, dir, cfg.HistoryRetentionRuns)
			}

			fmt.Fprint(cmd.OutOrStdout(), report)
			return nil
		},
	}
	cmd.Flags().StringVar(&complianceFramework, "compliance", "", `framework to map evidence onto: "nist-ai-rmf" or "eu-ai-act" (required)`)
	return cmd
}
