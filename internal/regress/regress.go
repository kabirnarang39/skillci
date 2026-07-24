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

type MatrixReport struct {
	Outcomes       []Outcome
	GeneratedCases []evalspec.Case
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
			result, err := runner.RunCase(ctx, client, skillDir, model, c, cfg.Pricing)
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
					report.GeneratedCases = append(report.GeneratedCases, evalspec.Case{
						Name:           c.Name + "-generated-" + model,
						Prompt:         c.Prompt,
						SkillUnderTest: c.SkillUnderTest,
						Assert:         c.Assert,
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
// skillDir, for later review via `skillci accept`.
func WriteGeneratedCases(skillDir string, cases []evalspec.Case) ([]string, error) {
	dir := filepath.Join(skillDir, "evals", "_generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var written []string
	for _, c := range cases {
		data, err := yaml.Marshal(c)
		if err != nil {
			return nil, err
		}
		safeName := strings.ReplaceAll(c.Name, "/", "-")
		path := filepath.Join(dir, safeName+".yaml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}
