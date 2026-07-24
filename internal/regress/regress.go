// Package regress implements the regression matrix engine: it runs every
// eval case against every model in the config's matrix and compares each
// result against the last recorded history run to decide whether a failure
// is a genuinely new regression, an already-known failure, or an uncovered
// gap that should grow into a tracked eval case (see design §5).
package regress

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kabirnarang39/skillci/internal/anthropic"
	"github.com/kabirnarang39/skillci/internal/config"
	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/history"
	"github.com/kabirnarang39/skillci/internal/runner"
	"gopkg.in/yaml.v3"
)

type Outcome struct {
	Case            evalspec.Case
	Model           string
	Result          runner.Result
	IsNewRegression bool
	// StrictDimensionFail is true when Case.Dimensions matches any
	// key/value pair in config.Config.StrictDimensions AND the case
	// failed — ShouldFailCI treats this as a hard CI failure regardless
	// of the configured FailOn policy.
	StrictDimensionFail bool
}

// matchesStrictDimensions reports whether dims matches ANY key/value pair
// configured in strict — OR across configured pairs, exact string
// equality, no wildcards. A nil/empty dims or strict never matches.
func matchesStrictDimensions(dims map[string]string, strict map[string][]string) bool {
	for key, values := range dims {
		for _, strictValue := range strict[key] {
			if values == strictValue {
				return true
			}
		}
	}
	return false
}

// GeneratedCase is an uncovered-failure case proposed by the self-growing
// eval loop, carrying the failure context that led to it — not just the
// bare name/prompt/assert a reviewer would otherwise have to go dig out of
// CI logs themselves.
type GeneratedCase struct {
	Case           evalspec.Case
	Model          string
	Timestamp      time.Time
	ActualResponse string
}

type MatrixReport struct {
	Outcomes       []Outcome
	GeneratedCases []GeneratedCase
}

func (r MatrixReport) ShouldFailCI(failOn string) bool {
	for _, o := range r.Outcomes {
		if o.StrictDimensionFail {
			return true
		}
		switch failOn {
		case "any_fail":
			if !o.Result.Passed {
				return true
			}
		case "triggered_only":
			if o.Case.Assert.Triggered != nil && o.Result.Triggered != *o.Case.Assert.Triggered {
				return true
			}
		default: // "regression"
			if o.IsNewRegression {
				return true
			}
		}
	}
	return false
}

// RunMatrix runs every case against every model in cfg.Models, comparing
// each result to the last recorded run in hist to decide whether a failure
// is a *new* regression (see design §5 / the self-growing eval rule in this
// task's header). It returns the report plus the history.Run that the
// caller should append and save.
func RunMatrix(ctx context.Context, client *anthropic.Client, skillDir string, cfg config.Config, cases []evalspec.Case, hist history.History) (MatrixReport, history.Run, error) {
	lastRun, hadHistory := hist.LastRun()

	var report MatrixReport
	newRun := history.Run{}

	for _, c := range cases {
		for _, model := range cfg.Models {
			result, err := runner.RunCase(ctx, client, skillDir, model, c, cfg.Pricing, cfg.JudgeModel)
			if err != nil {
				return MatrixReport{}, history.Run{}, fmt.Errorf("case %s on %s: %w", c.Name, model, err)
			}

			newRun.Cases = append(newRun.Cases, history.CaseResult{
				Name: c.Name, Model: model, Passed: result.Passed,
			})

			prior, hadPrior := lastRun.Result(c.Name, model)
			isNewRegression := false
			if !result.Passed {
				if hadPrior && prior.Passed {
					isNewRegression = true
				}
				// A snapshotting case already has its own review artifact
				// (the pending golden file) and review flow (`skillci diff`
				// / `skillci accept --model`) — don't also clone it into
				// the self-growing eval loop for the same failure.
				isSnapshotCase := c.Assert.Snapshot != nil && *c.Assert.Snapshot
				isFuzzStrictCase := c.Assert.FuzzStrict != nil && *c.Assert.FuzzStrict
				if !hadPrior && !isSnapshotCase && !isFuzzStrictCase {
					report.GeneratedCases = append(report.GeneratedCases, GeneratedCase{
						Case: evalspec.Case{
							Name:           c.Name + "-generated-" + model,
							Prompt:         c.Prompt,
							SkillUnderTest: c.SkillUnderTest,
							Assert:         c.Assert,
						},
						Model:          model,
						Timestamp:      time.Now(),
						ActualResponse: result.ResponseText,
					})
				}
			}
			_ = hadHistory

			report.Outcomes = append(report.Outcomes, Outcome{
				Case: c, Model: model, Result: result, IsNewRegression: isNewRegression,
				StrictDimensionFail: !result.Passed && matchesStrictDimensions(c.Dimensions, cfg.StrictDimensions),
			})
		}
	}

	return report, newRun, nil
}

// WriteGeneratedCases writes each case to evals/_generated/<name>.yaml under
// skillDir, for later review via `skillci accept`. Each file's case body is
// preceded by a YAML comment header recording the failure context that
// produced it (model, detection time, actual response) — comments are
// invisible to evalspec.LoadDir's parsing, so this is purely informational
// for a human reviewer, including after `accept` copies the file verbatim
// into evals/.
func WriteGeneratedCases(skillDir string, cases []GeneratedCase) ([]string, error) {
	dir := filepath.Join(skillDir, "evals", "_generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var written []string
	for _, gc := range cases {
		data, err := yaml.Marshal(gc.Case)
		if err != nil {
			return nil, err
		}
		full := append(failureContextHeader(gc), data...)
		safeName := strings.ReplaceAll(gc.Case.Name, "/", "-")
		path := filepath.Join(dir, safeName+".yaml")
		if err := os.WriteFile(path, full, 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

// failureContextHeader renders gc's failure context as a YAML comment
// block, safe to prepend directly before the marshaled case body.
func failureContextHeader(gc GeneratedCase) []byte {
	var b strings.Builder
	b.WriteString("# generated by skillci's self-growing eval loop — informational, not part of the case spec\n")
	fmt.Fprintf(&b, "# model: %s\n", gc.Model)
	fmt.Fprintf(&b, "# detected_at: %s\n", gc.Timestamp.UTC().Format(time.RFC3339))
	b.WriteString("# actual_response:\n")
	response := gc.ActualResponse
	if response == "" {
		response = "(empty)"
	}
	for line := range strings.SplitSeq(response, "\n") {
		fmt.Fprintf(&b, "#   %s\n", line)
	}
	return []byte(b.String())
}
