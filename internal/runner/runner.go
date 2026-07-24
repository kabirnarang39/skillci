package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/fuzz"
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
	FuzzFindings []fuzz.Finding
	// OutputTokens and LatencyMs are always populated, independent of
	// whether any assertion uses them.
	OutputTokens int
	LatencyMs    int64
	// LatencyExceeded is set whenever MaxLatencyMs is exceeded, regardless
	// of LatencyStrict — used for the non-blocking [LATENCY] report line.
	LatencyExceeded bool
	// FlakeVerdict is set only when FlakeRetries fired (the first attempt's
	// trigger checks failed and FlakeRetries > 0): "confirmed_pass",
	// "confirmed_fail", or "unstable" (no majority reached). Empty string
	// means flake retries never triggered — either FlakeRetries wasn't
	// set, or the first attempt already passed.
	FlakeVerdict        string
	FlakeAttemptsPassed int
	FlakeAttemptsTotal  int
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
func RunCase(ctx context.Context, client *anthropic.Client, skillDir, model string, c evalspec.Case, pricing map[string]config.ModelPricing) (Result, error) {
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

	triggered, content := parseTriggerMarker(msg.Text)

	result := Result{
		CaseName:     c.Name,
		Model:        model,
		Triggered:    triggered,
		InputTokens:  msg.InputTokens,
		OutputTokens: msg.OutputTokens,
		LatencyMs:    msg.Latency.Milliseconds(),
	}

	triggerMsgs := checkTriggerAssertions(triggered, content, c.Assert)
	budgetMsgs, latencyExceeded := checkBudgetAssertions(msg.InputTokens, msg.OutputTokens, result.LatencyMs, model, c.Assert, pricing)
	result.LatencyExceeded = latencyExceeded

	shouldFailOnTrigger := len(triggerMsgs) > 0
	if len(triggerMsgs) > 0 && c.Assert.FlakeRetries != nil && *c.Assert.FlakeRetries > 0 {
		verdict, passed, total, verr := voteOnFlakeRetries(ctx, client, model, systemPrompt, c)
		if verr != nil {
			return Result{}, verr
		}
		result.FlakeVerdict = verdict
		result.FlakeAttemptsPassed = passed
		result.FlakeAttemptsTotal = total
		switch verdict {
		case "confirmed_pass":
			shouldFailOnTrigger = false
		case "confirmed_fail":
			shouldFailOnTrigger = true
		case "unstable":
			shouldFailOnTrigger = c.Assert.FlakeStrict != nil && *c.Assert.FlakeStrict
		}
	}

	if shouldFailOnTrigger {
		result.Failures = append(result.Failures, triggerMsgs...)
	}
	result.Failures = append(result.Failures, budgetMsgs...)

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

	// Fuzzing only runs once every other assertion has already passed
	// (same guard as snapshot, same reasoning: don't waste model calls or
	// report noise on a case that's already failing for an unrelated
	// reason) and only when there's a Triggered expectation for a mutated
	// prompt's outcome to be compared against.
	if len(result.Failures) == 0 && c.Assert.Fuzz != nil && *c.Assert.Fuzz && c.Assert.Triggered != nil {
		for _, m := range fuzz.Generate(c.Prompt) {
			mMsg, err := client.Send(ctx, model, systemPrompt, m.Prompt)
			if err != nil {
				return Result{}, err
			}
			mTriggered, _ := parseTriggerMarker(mMsg.Text)
			result.FuzzFindings = append(result.FuzzFindings, fuzz.Finding{
				Mutation:  m,
				Triggered: mTriggered,
				Flipped:   mTriggered != *c.Assert.Triggered,
			})
		}
		flipped := 0
		for _, f := range result.FuzzFindings {
			if f.Flipped {
				flipped++
			}
		}
		if flipped > 0 && c.Assert.FuzzStrict != nil && *c.Assert.FuzzStrict {
			result.Failures = append(result.Failures, fmt.Sprintf("fuzz: %d/%d mutation(s) flipped trigger behavior", flipped, len(result.FuzzFindings)))
		}
	}

	result.Passed = len(result.Failures) == 0
	return result, nil
}

// checkTriggerAssertions checks Triggered/Contains/NotContains — the
// assertions eligible for FlakeRetries voting — against a single
// attempt's response, returning failure messages (nil when all pass).
func checkTriggerAssertions(triggered bool, content string, assert evalspec.Assertions) []string {
	var msgs []string
	if assert.Triggered != nil && triggered != *assert.Triggered {
		msgs = append(msgs, fmt.Sprintf("triggered = %v, want %v", triggered, *assert.Triggered))
	}
	for _, want := range assert.Contains {
		if !strings.Contains(content, want) {
			msgs = append(msgs, fmt.Sprintf("response missing required substring %q", want))
		}
	}
	for _, unwanted := range assert.NotContains {
		if strings.Contains(content, unwanted) {
			msgs = append(msgs, fmt.Sprintf("response contains forbidden substring %q", unwanted))
		}
	}
	return msgs
}

// checkBudgetAssertions checks MaxTokensLoaded/MaxOutputTokens/
// MaxLatencyMs/MaxCostUSD — always evaluated once, against the first
// attempt only, and never retried by FlakeRetries.
func checkBudgetAssertions(inputTokens, outputTokens int, latencyMs int64, model string, assert evalspec.Assertions, pricing map[string]config.ModelPricing) (msgs []string, latencyExceeded bool) {
	if assert.MaxTokensLoaded != nil && inputTokens > *assert.MaxTokensLoaded {
		msgs = append(msgs, fmt.Sprintf("input_tokens = %d, exceeds max_tokens_loaded %d", inputTokens, *assert.MaxTokensLoaded))
	}
	if assert.MaxOutputTokens != nil && outputTokens > *assert.MaxOutputTokens {
		msgs = append(msgs, fmt.Sprintf("output_tokens = %d, exceeds max_output_tokens %d", outputTokens, *assert.MaxOutputTokens))
	}
	if assert.MaxLatencyMs != nil && latencyMs > *assert.MaxLatencyMs {
		latencyExceeded = true
		if assert.LatencyStrict != nil && *assert.LatencyStrict {
			msgs = append(msgs, fmt.Sprintf("latency = %dms, exceeds max_latency_ms %d", latencyMs, *assert.MaxLatencyMs))
		}
	}
	if assert.MaxCostUSD != nil {
		price, ok := pricing[model]
		if !ok {
			msgs = append(msgs, fmt.Sprintf("no pricing configured for model %q — add it under pricing: in .skillci.yaml", model))
		} else {
			cost := float64(inputTokens)/1e6*price.InputPerMillion + float64(outputTokens)/1e6*price.OutputPerMillion
			if cost > *assert.MaxCostUSD {
				msgs = append(msgs, fmt.Sprintf("estimated cost = $%.4f, exceeds max_cost_usd %.4f", cost, *assert.MaxCostUSD))
			}
		}
	}
	return msgs, latencyExceeded
}

// voteOnFlakeRetries re-runs c's prompt up to c.Assert.FlakeRetries
// additional times (the caller has already made attempt 1, which failed
// its trigger checks — that's why this was called), taking a majority
// verdict across all attempts. It stops making further calls as soon as
// a majority is mathematically decided, to avoid spending the full
// budget when the outcome can no longer change.
func voteOnFlakeRetries(ctx context.Context, client *anthropic.Client, model, systemPrompt string, c evalspec.Case) (verdict string, passed, total int, err error) {
	maxAttempts := 1 + *c.Assert.FlakeRetries
	attemptPassed := []bool{false} // attempt 1 already known to have failed

	for len(attemptPassed) < maxAttempts {
		passes, fails := countPassFail(attemptPassed)
		remaining := maxAttempts - len(attemptPassed)
		if passes > fails+remaining || fails > passes+remaining {
			break
		}
		msg, sendErr := client.Send(ctx, model, systemPrompt, c.Prompt)
		if sendErr != nil {
			return "", 0, 0, sendErr
		}
		triggered, content := parseTriggerMarker(msg.Text)
		attemptPassed = append(attemptPassed, len(checkTriggerAssertions(triggered, content, c.Assert)) == 0)
	}

	passes, fails := countPassFail(attemptPassed)
	switch {
	case passes > fails:
		verdict = "confirmed_pass"
	case fails > passes:
		verdict = "confirmed_fail"
	default:
		verdict = "unstable"
	}
	return verdict, passes, len(attemptPassed), nil
}

func countPassFail(attempts []bool) (passes, fails int) {
	for _, p := range attempts {
		if p {
			passes++
		} else {
			fails++
		}
	}
	return passes, fails
}

// parseTriggerMarker splits the model's response into whether the skill
// would have triggered and the response content to check assertions
// against (empty when the model reports it would not have triggered).
func parseTriggerMarker(text string) (triggered bool, content string) {
	firstLine, remainder, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(firstLine) == triggerMarkerPrefix+" true" {
		return true, remainder
	}
	if strings.TrimSpace(strings.TrimSpace(text)) == triggerMarkerPrefix+" false" {
		return false, ""
	}
	return false, text
}
