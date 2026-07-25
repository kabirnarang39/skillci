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
			attackPrompt := c.Prompt + plugin.AttackPromptSuffix
			msg, err := client.Send(ctx, model, systemPrompt, attackPrompt)
			if err != nil {
				return nil, err
			}
			_, content := parseTriggerMarker(msg.Text)
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
