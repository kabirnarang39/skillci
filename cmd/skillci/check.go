package main

import (
	"fmt"

	"github.com/kabirnarang39/skillci/internal/lint"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [path]",
		Short: "Lint a skill's SKILL.md and referenced files (no API calls)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			issues, err := lint.LintSkill(dir)
			if err != nil {
				return err
			}
			for _, iss := range issues {
				fmt.Fprintf(cmd.OutOrStdout(), "%s:%d: %s: %s\n", iss.File, iss.Line, iss.Rule, iss.Msg)
			}
			if len(issues) > 0 {
				return fmt.Errorf("%d lint issue(s) found", len(issues))
			}
			return nil
		},
	}
}
