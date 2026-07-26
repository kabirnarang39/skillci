package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kabirnarang39/skillci/internal/mcpserver"
	"github.com/spf13/cobra"
)

// runCobraCommand executes a real cobra command in-process — the same
// command object the CLI itself runs — and captures its output, so an MCP
// tool call can never behave differently from running `skillci <cmd>`
// directly. isError mirrors the command's own exit code (true whenever
// Execute returns a non-nil error), matching how every other skillci
// command already signals failure to a caller.
func runCobraCommand(newCmd func() *cobra.Command, args []string) (string, bool) {
	cmd := newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()
	text := out.String()
	if err != nil {
		if text != "" {
			text += "\n"
		}
		text += "error: " + err.Error()
		return text, true
	}
	if text == "" {
		text = "(no output)"
	}
	return text, false
}

// requireString reports a protocol-level error (bad tools/call arguments,
// not a tool execution failure) for a required field left empty — the
// same distinction Handler's own doc comment describes.
func requireString(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

func schema(properties map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func stringProp(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func boolProp(description string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": description}
}

// mcpTools is the full set of skillci commands exposed over MCP — every
// command except mcp-serve itself. Each Handler invokes the real cobra
// command (see runCobraCommand) rather than reimplementing its logic, so
// this registry can never silently drift from what the CLI actually does
// as commands gain new flags over time.
func mcpTools() []mcpserver.Tool {
	return []mcpserver.Tool{
		{
			Name:        "init",
			Title:       "Scaffold a new skill's skillci config",
			Description: "Scaffold .skillci.yaml and an example eval case inside a skill's directory. Run this once, the first time a skill gets eval coverage.",
			InputSchema: schema(map[string]interface{}{
				"path": stringProp("Path to the skill's directory to scaffold into."),
			}, "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				text, isErr := runCobraCommand(newInitCmd, []string{a.Path})
				return text, isErr, nil
			},
		},
		{
			Name:        "check",
			Title:       "Lint a Claude Skill",
			Description: "Lint a Claude Skill's SKILL.md (frontmatter, structure, OWASP Agentic Skills Top 10-mapped security patterns, skill-bloat checks) and its eval cases. Local-only, zero API calls unless verify_pinned_sources is set. Run this after creating or editing any SKILL.md, before treating the change as done.",
			InputSchema: schema(map[string]interface{}{
				"path":                  stringProp("Path to the skill's directory (the one containing SKILL.md)."),
				"mode":                  map[string]interface{}{"type": "string", "enum": []string{"block", "warn"}, "description": `Override lint severity: "warn" reports every issue without failing, "block" fails on any issue. Omit to use .skillci.yaml's lint policy (default: block).`},
				"format":                map[string]interface{}{"type": "string", "enum": []string{"text", "json"}, "description": `Output format. Defaults to "text".`},
				"verify_pinned_sources": boolProp("Fetch each pinned_sources URL over the network and verify its content hash hasn't changed. The only way this tool makes a network call; off by default."),
			}, "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path                string `json:"path"`
					Mode                string `json:"mode"`
					Format              string `json:"format"`
					VerifyPinnedSources bool   `json:"verify_pinned_sources"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.Path}
				if a.Mode != "" {
					args = append(args, "--mode", a.Mode)
				}
				if a.Format != "" {
					args = append(args, "--format", a.Format)
				}
				if a.VerifyPinnedSources {
					args = append(args, "--verify-pinned-sources")
				}
				text, isErr := runCobraCommand(newCheckCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "eval",
			Title:       "Run a Claude Skill's eval suite",
			Description: "Run a skill's eval suite (evals/*.yaml) against a single Claude model. Costs API calls — requires ANTHROPIC_API_KEY in this server's own environment. Run this when eval cases exist and you've changed the skill's behavior or trigger phrases.",
			InputSchema: schema(map[string]interface{}{
				"path":  stringProp("Path to the skill's directory (the one containing SKILL.md and evals/)."),
				"model": stringProp(`Model to evaluate against. Defaults to "claude-sonnet-5".`),
			}, "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path  string `json:"path"`
					Model string `json:"model"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.Path}
				if a.Model != "" {
					args = append(args, "--model", a.Model)
				}
				text, isErr := runCobraCommand(newEvalCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "regress",
			Title:       "Run the full regression matrix",
			Description: "Run the eval suite across the configured model matrix (.skillci.yaml), diffed against the last known-good run, and fail only on a *new* regression. Writes .skillci/history.json and evals/_generated/*.yaml for uncovered failures — this is the command a CI pipeline runs, not a read-only check. auto_bisect and open_pr have real side effects (running git bisect, or committing + opening a pull request) — leave them off unless you specifically want that.",
			InputSchema: schema(map[string]interface{}{
				"path":        stringProp("Path to the skill's directory."),
				"upload":      boolProp("Upload results to the skillci dashboard, if configured. Off by default."),
				"auto_bisect": boolProp("Automatically run bisect on every new regression instead of just printing the suggested command. Off by default."),
				"open_pr":     boolProp("Commit any generated eval case(s) to a new branch and open a pull request, instead of just writing a file. Off by default — this mutates the repo's remote state."),
			}, "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path       string `json:"path"`
					Upload     bool   `json:"upload"`
					AutoBisect bool   `json:"auto_bisect"`
					OpenPR     bool   `json:"open_pr"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.Path}
				if a.Upload {
					args = append(args, "--upload")
				}
				if a.AutoBisect {
					args = append(args, "--auto-bisect")
				}
				if a.OpenPR {
					args = append(args, "--open-pr")
				}
				text, isErr := runCobraCommand(newRegressCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "fuzz",
			Title:       "Run fuzz-enabled eval cases",
			Description: "Run mutation-based robustness testing (deterministic paraphrase mutations) for a skill's fuzz-enabled eval cases only. Costs API calls.",
			InputSchema: schema(map[string]interface{}{
				"path":  stringProp("Path to the skill's directory."),
				"model": stringProp(`Model to evaluate against. Defaults to "claude-sonnet-5".`),
			}, "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path  string `json:"path"`
					Model string `json:"model"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.Path}
				if a.Model != "" {
					args = append(args, "--model", a.Model)
				}
				text, isErr := runCobraCommand(newFuzzCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "bisect",
			Title:       "Binary-search git history for a regression's culprit commit",
			Description: "Binary-search a skill's own git history (using a real git worktree) to find which commit broke a specific eval case. Requires the skill directory to be inside a git repository with real commit history. Costs API calls (one eval per commit checked).",
			InputSchema: schema(map[string]interface{}{
				"case_name": stringProp("The eval case's name to bisect."),
				"path":      stringProp("Path to the skill's directory."),
				"model":     stringProp(`Model to evaluate against. Defaults to "claude-sonnet-5".`),
				"good":      stringProp("Known-good commit SHA. Auto-detected from history.json if omitted."),
				"bad":       stringProp("Known-bad commit SHA. Defaults to the most recent recorded run, or current HEAD, if omitted."),
			}, "case_name", "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					CaseName string `json:"case_name"`
					Path     string `json:"path"`
					Model    string `json:"model"`
					Good     string `json:"good"`
					Bad      string `json:"bad"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("case_name", a.CaseName); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.CaseName, "--path", a.Path}
				if a.Model != "" {
					args = append(args, "--model", a.Model)
				}
				if a.Good != "" {
					args = append(args, "--good", a.Good)
				}
				if a.Bad != "" {
					args = append(args, "--bad", a.Bad)
				}
				text, isErr := runCobraCommand(newBisectCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "accept",
			Title:       "Accept a generated eval case or pending snapshot change",
			Description: "Promote a generated eval case (evals/_generated/<name>.yaml, written by regress when it hits an uncovered failure) into permanent coverage, or — with model set — promote a pending snapshot change into the new accepted golden baseline.",
			InputSchema: schema(map[string]interface{}{
				"case_name": stringProp("The generated case's name, or the case name whose snapshot change to accept."),
				"path":      stringProp("Path to the skill's directory."),
				"model":     stringProp("Model whose pending snapshot change to accept. Omit to accept a generated eval case instead."),
			}, "case_name", "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					CaseName string `json:"case_name"`
					Path     string `json:"path"`
					Model    string `json:"model"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("case_name", a.CaseName); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.CaseName, "--path", a.Path}
				if a.Model != "" {
					args = append(args, "--model", a.Model)
				}
				text, isErr := runCobraCommand(newAcceptCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "diff",
			Title:       "Show a case's pending snapshot diff",
			Description: "Show a case's pending snapshot change against its accepted golden baseline, without accepting it.",
			InputSchema: schema(map[string]interface{}{
				"case_name": stringProp("The eval case's name."),
				"path":      stringProp("Path to the skill's directory."),
				"model":     stringProp(`Model to inspect. Defaults to "claude-sonnet-5".`),
			}, "case_name", "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					CaseName string `json:"case_name"`
					Path     string `json:"path"`
					Model    string `json:"model"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("case_name", a.CaseName); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				args := []string{a.CaseName, "--path", a.Path}
				if a.Model != "" {
					args = append(args, "--model", a.Model)
				}
				text, isErr := runCobraCommand(newDiffCmd, args)
				return text, isErr, nil
			},
		},
		{
			Name:        "badge",
			Title:       "Regenerate the status badge",
			Description: "Regenerate the SVG compatibility badge from the latest recorded run in .skillci/history.json.",
			InputSchema: schema(map[string]interface{}{
				"path": stringProp("Path to the skill's directory."),
			}, "path"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				text, isErr := runCobraCommand(newBadgeCmd, []string{a.Path})
				return text, isErr, nil
			},
		},
		{
			Name:        "report",
			Title:       "Generate a compliance evidence report",
			Description: `Generate a Markdown evidence report mapping a skill's eval cases and run history onto a governance framework's own documentation/testing requirements ("nist-ai-rmf" or "eu-ai-act"). This is evidence, not certification — skillci cannot certify a skill compliant with anything.`,
			InputSchema: schema(map[string]interface{}{
				"path":       stringProp("Path to the skill's directory."),
				"compliance": map[string]interface{}{"type": "string", "enum": []string{"nist-ai-rmf", "eu-ai-act"}, "description": "Which framework to map evidence onto."},
			}, "path", "compliance"),
			Handler: func(arguments json.RawMessage) (string, bool, error) {
				var a struct {
					Path       string `json:"path"`
					Compliance string `json:"compliance"`
				}
				if err := json.Unmarshal(arguments, &a); err != nil {
					return "", false, err
				}
				if err := requireString("path", a.Path); err != nil {
					return "", false, err
				}
				if err := requireString("compliance", a.Compliance); err != nil {
					return "", false, err
				}
				args := []string{"--compliance", a.Compliance, a.Path}
				text, isErr := runCobraCommand(newReportCmd, args)
				return text, isErr, nil
			},
		},
	}
}
