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
		Long: `Lint a skill's SKILL.md and referenced files (no API calls).

Includes a first-layer static security scan mapped to OWASP's Agentic
Skills Top 10: AST01 (malicious payloads), AST03 (over-privileged access),
AST04 (insecure metadata parsing), AST10 (cross-platform format issues).
This is pattern-matching, not a malware scanner — obfuscated or
natural-language-only attacks can bypass it (OWASP AST08).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			issues, err := lint.LintSkill(dir)
			if err != nil {
				return err
			}
			evalIssues, err := lint.LintEvals(dir)
			if err != nil {
				return err
			}
			issues = append(issues, evalIssues...)
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
