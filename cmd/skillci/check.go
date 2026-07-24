package main

import (
	"encoding/json"
	"fmt"

	"github.com/kabirnarang39/skillci/internal/lint"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Lint a skill's SKILL.md and referenced files (no API calls)",
		Long: `Lint a skill's SKILL.md and referenced files (no API calls).

Includes a first-layer static security scan mapped to OWASP's Agentic
Skills Top 10: AST01 (malicious payloads), AST02 (unpinned dependencies),
AST03 (over-privileged access), AST04 (insecure metadata parsing), AST05
(untrusted external instructions), AST10 (cross-platform format issues).
This is pattern-matching, not a malware scanner — obfuscated or
natural-language-only attacks can bypass it (OWASP AST08).`,
		// A lint failure (issues found) is a normal, expected outcome for
		// this command — not user misuse of the CLI's flags/args — so
		// cobra's default "print the Usage block on any RunE error" would
		// otherwise land noise in the output every single time issues are
		// found. That noise is actively harmful for --format json
		// specifically: it writes into the same stream as the JSON, on
		// the same buffer a caller reads, corrupting what's supposed to
		// be a clean parseable payload for an editor extension or script.
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("--format must be \"text\" or \"json\", got %q", format)
			}

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

			if format == "json" {
				// Emit [] rather than JSON `null` for a clean pass — nil
				// slices marshal to null, which is a needless special case
				// for a machine reader (an editor extension, a script) to
				// have to handle alongside the empty-array case.
				out := issues
				if out == nil {
					out = []lint.Issue{}
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(out); err != nil {
					return err
				}
			} else {
				for _, iss := range issues {
					fmt.Fprintf(cmd.OutOrStdout(), "%s:%d: %s: %s\n", iss.File, iss.Line, iss.Rule, iss.Msg)
				}
			}

			if len(issues) > 0 {
				return fmt.Errorf("%d lint issue(s) found", len(issues))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	return cmd
}
