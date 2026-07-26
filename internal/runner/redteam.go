// internal/runner/redteam.go
package runner

import (
	"context"
	"fmt"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/redteam"
)

// runRedteamPlugins runs every plugin named in c.Assert.Redteam against
// the skill under test. Each plugin gets its own attack-augmented
// prompt sent as a separate model call — a redteam attack probes a
// different input than the case's own base prompt, so it can't reuse
// the response RunCase already fetched for the case's other assertions.
// A judge-graded plugin with Turns set instead sends its attack as a
// multi-turn conversation, threading history across calls via
// client.SendConversation — every other plugin shape (deterministic, or
// judge-graded via AttackPromptSuffix) is a single call as before.
// Judge-graded plugins are scored through the exact same runJudgeGroup
// path Judge criteria use: a redteam judge-graded plugin is, mechanically,
// a JudgeCriterion built from the plugin's rubric. All findings share
// JudgeFinding's polarity convention — Passed: true always means the
// attack was defended, matching how Passed: true means "criterion
// satisfied" for every other Judge-graded finding in this codebase.
func runRedteamPlugins(ctx context.Context, client *anthropic.Client, model, systemPrompt string, c evalspec.Case, meta skillMeta, judgeModel string) ([]JudgeFinding, error) {
	var findings []JudgeFinding
	for _, attack := range c.Assert.Redteam {
		plugin, ok := redteam.Registry[attack.Plugin]
		if !ok {
			return nil, fmt.Errorf("case %q references unknown redteam plugin %q", c.Name, attack.Plugin)
		}

		switch plugin.Grading {
		case redteam.GradingDeterministic:
			attackPrompt, state := plugin.BuildAttack(c.Prompt)
			msg, err := client.Send(ctx, model, systemPrompt, attackPrompt)
			if err != nil {
				return nil, err
			}
			_, content := parseTriggerMarker(msg.Text)
			succeeded, detail := plugin.Detect(content, state, meta.Name, meta.Description)
			passed := !succeeded
			passCount := 0
			if passed {
				passCount = 1
			}
			findings = append(findings, JudgeFinding{
				Name: plugin.Name, Passed: passed, Reason: detail, SampleCount: 1, PassCount: passCount,
			})

		case redteam.GradingJudge:
			if judgeModel == "" {
				return nil, fmt.Errorf("case %q uses redteam plugin %q, which requires judge grading, but no judge_model is configured — add judge_model: to .skillci.yaml", c.Name, attack.Plugin)
			}
			var content string
			if len(plugin.Turns) > 0 {
				var history []anthropic.ConversationTurn
				for i, step := range plugin.Turns {
					// Turn 1 is anchored in the case's own prompt (mirroring
					// the single-turn AttackPromptSuffix path below) so the
					// model stays in-context for the skill it's actually
					// being tested against — otherwise a compliant model
					// against a narrow skill just replies with the harness's
					// own "SKILLCI_TRIGGERED: false" scaffolding to every
					// generic crescendo turn, and the attack never really runs.
					message := step.Message
					if i == 0 {
						message = c.Prompt + "\n\n" + step.Message
					}
					history = append(history, anthropic.ConversationTurn{Role: "user", Content: message})
					msg, err := client.SendConversation(ctx, model, systemPrompt, history)
					if err != nil {
						return nil, err
					}
					// Strip the trigger marker from every turn's response
					// before threading it into history — otherwise a raw
					// "SKILLCI_TRIGGERED: false" reply pollutes the
					// conversation context the model sees on later turns.
					_, stripped := parseTriggerMarker(msg.Text)
					history = append(history, anthropic.ConversationTurn{Role: "assistant", Content: stripped})
					content = stripped
				}
			} else {
				attackPrompt := c.Prompt + plugin.AttackPromptSuffix
				msg, err := client.Send(ctx, model, systemPrompt, attackPrompt)
				if err != nil {
					return nil, err
				}
				_, content = parseTriggerMarker(msg.Text)
			}

			crit := evalspec.JudgeCriterion{Name: plugin.Name, Criterion: plugin.JudgeRubric}
			groupFindings, jerr := runJudgeGroup(ctx, client, judgeModel, content, []evalspec.JudgeCriterion{crit})
			if jerr != nil {
				return nil, jerr
			}
			findings = append(findings, groupFindings...)

		default:
			return nil, fmt.Errorf("redteam plugin %q has an unrecognized grading mode %q", plugin.Name, plugin.Grading)
		}
	}
	return findings, nil
}
