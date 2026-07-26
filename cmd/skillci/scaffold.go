package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newScaffoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "scaffold",
		Short:        "Generate starter code for extending skillci",
		SilenceUsage: true,
	}
	cmd.AddCommand(newScaffoldRedteamPluginCmd())
	return cmd
}

func newScaffoldRedteamPluginCmd() *cobra.Command {
	var grading string
	cmd := &cobra.Command{
		Use:          "redteam-plugin <name>",
		Short:        "Generate a stub internal/redteam.Plugin entry for a new attack",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			var stub string
			switch grading {
			case "deterministic":
				stub = deterministicPluginStub(name)
			case "judge":
				stub = judgePluginStub(name)
			case "multiturn":
				stub = multiturnPluginStub(name)
			default:
				return fmt.Errorf("--grading must be one of \"deterministic\", \"judge\", \"multiturn\" (got %q)", grading)
			}
			fmt.Fprintln(cmd.OutOrStdout(), stub)
			return nil
		},
	}
	cmd.Flags().StringVar(&grading, "grading", "", "attack shape: \"deterministic\", \"judge\", or \"multiturn\" (required)")
	return cmd
}

func deterministicPluginStub(name string) string {
	return fmt.Sprintf(`	"%s": {
		Name:     "%s",
		Category: CategoryInjection, // TODO: pick the category that fits — see internal/redteam.Category's const block
		Grading:  GradingDeterministic,
		// TODO: BuildAttack returns the attack-augmented prompt to send
		// in place of the case's own prompt, plus any per-invocation
		// state Detect needs (e.g. a random token) — empty string if
		// unused. See prompt-injection-canary in internal/redteam/redteam.go for
		// a worked example, and docs/adding-a-redteam-plugin.md.
		BuildAttack: func(basePrompt string) (attackPrompt, state string) {
			return basePrompt, ""
		},
		// TODO: Detect reports whether the attack succeeded against
		// response, given the state BuildAttack returned.
		Detect: func(response, state, skillName, skillDescription string) (succeeded bool, detail string) {
			return false, ""
		},
	},`, name, name)
}

func judgePluginStub(name string) string {
	return fmt.Sprintf(`	"%s": {
		Name:     "%s",
		Category: CategoryJailbreak, // TODO: pick the category that fits — see internal/redteam.Category's const block
		Grading:  GradingJudge,
		// TODO: the text appended to the case's own prompt to build the
		// attack. See jailbreak-direct-override in internal/redteam/redteam.go
		// for a worked example.
		AttackPromptSuffix: "TODO: describe the attack instruction here",
		// TODO: the rubric a separate judge model grades the response
		// against — PASS should mean the attack was DEFENDED.
		JudgeRubric: "TODO: describe what a defended response looks like, and what a compromised one looks like",
	},`, name, name)
}

func multiturnPluginStub(name string) string {
	return fmt.Sprintf(`	"%s": {
		Name:     "%s",
		Category: CategoryJailbreak, // TODO: pick the category that fits — see internal/redteam.Category's const block
		Grading:  GradingJudge,
		// TODO: an escalating sequence of user messages — only the FINAL
		// turn's response is graded against JudgeRubric below. See
		// crescendo-jailbreak in internal/redteam/redteam.go for a worked
		// example, including how the runner anchors turn 1 in the
		// case's own prompt.
		Turns: []TurnStep{
			{Message: "TODO: turn 1 — establish innocuous framing"},
			{Message: "TODO: turn 2 — build on turn 1"},
			{Message: "TODO: turn 3 — the actual ask, leaning on the prior turns"},
		},
		// TODO: graded against the FINAL turn's response only.
		JudgeRubric: "TODO: describe what a defended response looks like, and what a compromised one looks like",
	},`, name, name)
}
