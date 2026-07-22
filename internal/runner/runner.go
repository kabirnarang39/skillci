package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/snapshot"
	"gopkg.in/yaml.v3"
)

type Result struct {
	CaseName     string
	Model        string
	Triggered    bool
	Passed       bool
	Failures     []string
	InputTokens  int
	SnapshotDiff *snapshot.Diff
}

type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func readSkillMeta(skillDir string) (skillMeta, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return skillMeta{}, err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return skillMeta{}, fmt.Errorf("SKILL.md missing frontmatter")
	}
	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		return skillMeta{}, fmt.Errorf("SKILL.md frontmatter not closed")
	}
	var meta skillMeta
	if err := yaml.Unmarshal([]byte(rest[:idx]), &meta); err != nil {
		return skillMeta{}, err
	}
	return meta, nil
}

const triggerMarkerPrefix = "SKILLCI_TRIGGERED:"

// RunCase sends the case's prompt to model, with a system prompt containing
// only the skill's name+description (a proxy for progressive-disclosure
// candidate matching — see Task 7 header note), then checks the response
// against the case's assertions.
func RunCase(ctx context.Context, client *anthropic.Client, skillDir, model string, c evalspec.Case) (Result, error) {
	meta, err := readSkillMeta(skillDir)
	if err != nil {
		return Result{}, err
	}

	systemPrompt := fmt.Sprintf(`You are Claude, deciding whether to use an available skill.

Skill available:
name: %s
description: %s

If, given the user's message, you would invoke this skill, begin your response with the exact line "%s true" followed by a newline, then respond as the skill would. If you would NOT invoke this skill for this message, respond with exactly "%s false" and nothing else.`, meta.Name, meta.Description, triggerMarkerPrefix, triggerMarkerPrefix)

	msg, err := client.Send(ctx, model, systemPrompt, c.Prompt)
	if err != nil {
		return Result{}, err
	}

	triggered := false
	content := msg.Text
	firstLine, remainder, _ := strings.Cut(msg.Text, "\n")
	if strings.TrimSpace(firstLine) == triggerMarkerPrefix+" true" {
		triggered = true
		content = remainder
	} else if strings.TrimSpace(strings.TrimSpace(msg.Text)) == triggerMarkerPrefix+" false" {
		triggered = false
		content = ""
	}

	result := Result{
		CaseName:    c.Name,
		Model:       model,
		Triggered:   triggered,
		InputTokens: msg.InputTokens,
	}

	if c.Assert.Triggered != nil && triggered != *c.Assert.Triggered {
		result.Failures = append(result.Failures, fmt.Sprintf("triggered = %v, want %v", triggered, *c.Assert.Triggered))
	}
	for _, want := range c.Assert.Contains {
		if !strings.Contains(content, want) {
			result.Failures = append(result.Failures, fmt.Sprintf("response missing required substring %q", want))
		}
	}
	for _, unwanted := range c.Assert.NotContains {
		if strings.Contains(content, unwanted) {
			result.Failures = append(result.Failures, fmt.Sprintf("response contains forbidden substring %q", unwanted))
		}
	}
	if c.Assert.MaxTokensLoaded != nil && msg.InputTokens > *c.Assert.MaxTokensLoaded {
		result.Failures = append(result.Failures, fmt.Sprintf("input_tokens = %d, exceeds max_tokens_loaded %d", msg.InputTokens, *c.Assert.MaxTokensLoaded))
	}

	// Only capture/compare a snapshot when every other assertion has
	// already passed. Otherwise a case that e.g. unexpectedly failed to
	// trigger would save its empty/garbage response as the golden
	// baseline (see final-review bug: empty-golden poisoning).
	if len(result.Failures) == 0 && c.Assert.Snapshot != nil && *c.Assert.Snapshot {
		golden, ok, err := snapshot.Load(skillDir, c.Name, model)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			if err := snapshot.Save(skillDir, c.Name, model, content); err != nil {
				return Result{}, err
			}
		} else {
			diff := snapshot.Compute(golden, content)
			if diff.Changed {
				result.SnapshotDiff = &diff
				if err := snapshot.SavePending(skillDir, c.Name, model, content); err != nil {
					return Result{}, err
				}
				if c.Assert.SnapshotStrict != nil && *c.Assert.SnapshotStrict {
					result.Failures = append(result.Failures, fmt.Sprintf("snapshot changed: %d word(s) differ from golden baseline", diff.WordsChanged))
				}
			}
		}
	}

	result.Passed = len(result.Failures) == 0
	return result, nil
}
