package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/lint"
	"github.com/spf13/cobra"
)

// reportedIssue adds the resolved severity to a lint.Issue for check's
// own output — kept out of the lint package since severity is a
// .skillci.yaml/--mode policy decision, not a fact the lint engine itself
// knows. Embedding (rather than a Severity field on lint.Issue) means
// existing json.Unmarshal-into-[]lint.Issue callers (editor extension,
// scripts, this repo's own tests) keep working unchanged — the extra
// field is additive, not a breaking shape change.
type reportedIssue struct {
	lint.Issue
	Severity string `json:"severity"`
}

func newCheckCmd() *cobra.Command {
	var format string
	var verifyPinnedSources bool
	var mode string
	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Lint a skill's SKILL.md and referenced files (no API calls)",
		Long: `Lint a skill's SKILL.md and referenced files (no API calls).

Includes a first-layer static security scan mapped to OWASP's Agentic
Skills Top 10: AST01 (malicious payloads), AST02 (unpinned dependencies),
AST03 (over-privileged access), AST04 (insecure metadata parsing), AST05
(untrusted external instructions), AST10 (cross-platform format issues).
This is pattern-matching, not a malware scanner — obfuscated or
natural-language-only attacks can bypass it (OWASP AST08).

By default this command makes zero network calls. --verify-pinned-sources
is the one opt-in exception: it fetches each URL a skill's frontmatter
declares under pinned_sources and confirms the content hash hasn't
silently changed since it was pinned. Off unless you ask for it by name.

Every issue is "block" severity (fails the command) unless .skillci.yaml's
lint.mode/lint.rules or this command's own --mode flag says otherwise —
lets a team pilot skillci non-blocking in CI, or promote specific rules to
blocking one at a time, instead of an all-or-nothing cutover.`,
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
			if mode != "" && mode != "block" && mode != "warn" {
				return fmt.Errorf(`--mode must be "block" or "warn", got %q`, mode)
			}

			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			cfg, err := config.Load(filepath.Join(dir, ".skillci.yaml"))
			if err != nil {
				return err
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

			if verifyPinnedSources {
				pinIssues, err := lint.VerifyPinnedSources(cmd.Context(), filepath.Join(dir, "SKILL.md"))
				if err != nil {
					return err
				}
				issues = append(issues, pinIssues...)
			}

			reported := make([]reportedIssue, len(issues))
			blocking := 0
			for i, iss := range issues {
				severity := mode
				if severity == "" {
					severity = cfg.Lint.Severity(iss.Rule)
				}
				if severity == "block" {
					blocking++
				}
				reported[i] = reportedIssue{Issue: iss, Severity: severity}
			}

			if format == "json" {
				// reported is built via make([]reportedIssue, len(issues)),
				// which is never nil even for zero issues — so this always
				// marshals to [] rather than JSON `null` for a clean pass,
				// with no extra nil-check needed to guarantee it.
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(reported); err != nil {
					return err
				}
			} else {
				for _, ri := range reported {
					tag := ""
					if ri.Severity == "warn" {
						tag = " (warn)"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s:%d: %s: %s%s\n", ri.File, ri.Line, ri.Rule, ri.Msg, tag)
				}
				// Only in text mode: a clean-exit-but-not-silent summary so
				// warn-only findings stay visible instead of looking like a
				// pass with nothing to say. JSON output stays a pure array —
				// no stray line to corrupt a machine reader's parse.
				if len(reported) > 0 && blocking == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "%d lint issue(s) found, all warn-only — not failing the build\n", len(reported))
				}
			}

			if blocking > 0 {
				return fmt.Errorf("%d lint issue(s) found (%d blocking, %d warned)", len(reported), blocking, len(reported)-blocking)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	cmd.Flags().BoolVar(&verifyPinnedSources, "verify-pinned-sources", false, "fetch each pinned_sources URL over the network and verify its content hash hasn't changed (the only network call this command ever makes; off by default)")
	cmd.Flags().StringVar(&mode, "mode", "", `override lint severity globally: "warn" (report but never fail) or "block" (fail on any issue) — takes precedence over .skillci.yaml's lint.mode/lint.rules; omit to use config (default: block)`)
	return cmd
}
