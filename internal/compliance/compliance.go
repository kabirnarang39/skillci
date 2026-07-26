// Package compliance turns a skill's existing eval cases and run history
// into an evidence report mapped onto a governance framework's own
// documentation/testing requirements — NIST AI RMF or the EU AI Act.
//
// This is evidence, not certification: skillci cannot certify anything.
// It can only report what testing/logging artifacts already exist for a
// skill, the same way a coverage report doesn't certify a codebase is
// bug-free. Every Render function says so explicitly, and every citation
// below was checked against the framework's own primary-source text
// (airc.nist.gov, artificialintelligenceact.eu) rather than paraphrased
// from memory or a secondary source.
package compliance

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kabirnarang39/skillci/internal/evalspec"
	"github.com/kabirnarang39/skillci/internal/history"
)

// Evidence summarizes what a skill's eval cases and run history actually
// contain — the raw material both Render functions map onto framework
// requirements. Gathered once so both report types read from the same
// counts.
type Evidence struct {
	TotalCases           int
	CasesWithTriggered   int
	CasesWithJudge       int
	TotalJudgeCriteria   int
	CasesWithRedteam     int
	RedteamPlugins       []string // sorted, unique
	CasesWithFuzz        int
	CasesWithSnapshot    int
	CasesWithBudget      int // max_tokens_loaded / max_output_tokens / max_latency_ms / max_cost_usd
	TotalRuns            int
	OldestRun, NewestRun time.Time
	HasRuns              bool
	ModelsCovered        []string // sorted, unique, drawn from recorded run results
}

// Gather computes Evidence from a skill's loaded eval cases and history.
func Gather(cases []evalspec.Case, hist history.History) Evidence {
	ev := Evidence{TotalCases: len(cases)}

	pluginSet := map[string]bool{}
	for _, c := range cases {
		a := c.Assert
		if a.Triggered != nil {
			ev.CasesWithTriggered++
		}
		if len(a.Judge) > 0 {
			ev.CasesWithJudge++
			ev.TotalJudgeCriteria += len(a.Judge)
		}
		if len(a.Redteam) > 0 {
			ev.CasesWithRedteam++
			for _, r := range a.Redteam {
				pluginSet[r.Plugin] = true
			}
		}
		if (a.Fuzz != nil && *a.Fuzz) || (a.FuzzLLM != nil && *a.FuzzLLM) {
			ev.CasesWithFuzz++
		}
		if a.Snapshot != nil && *a.Snapshot {
			ev.CasesWithSnapshot++
		}
		if a.MaxTokensLoaded != nil || a.MaxOutputTokens != nil || a.MaxLatencyMs != nil || a.MaxCostUSD != nil {
			ev.CasesWithBudget++
		}
	}
	for plugin := range pluginSet {
		ev.RedteamPlugins = append(ev.RedteamPlugins, plugin)
	}
	sort.Strings(ev.RedteamPlugins)

	ev.TotalRuns = len(hist.Runs)
	modelSet := map[string]bool{}
	for i, run := range hist.Runs {
		if i == 0 {
			ev.OldestRun = run.Timestamp
		}
		ev.NewestRun = run.Timestamp
		for _, c := range run.Cases {
			modelSet[c.Model] = true
		}
	}
	ev.HasRuns = ev.TotalRuns > 0
	for model := range modelSet {
		ev.ModelsCovered = append(ev.ModelsCovered, model)
	}
	sort.Strings(ev.ModelsCovered)

	return ev
}

const disclaimer = `> **This is evidence, not certification.** skillci cannot certify a skill
> compliant with any framework — it can only report what testing and
> logging artifacts already exist. Treat this as an index for an auditor
> or governance reviewer to inspect, not a pass/fail verdict.`

// RenderNISTAIRMF maps Evidence onto NIST AI RMF's Measure function
// subcategories. Subcategory text below is quoted from the framework's
// own published core (airc.nist.gov/airmf-resources/airmf/5-sec-core/),
// verified against that primary source rather than a secondary summary.
func RenderNISTAIRMF(ev Evidence, skillDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# NIST AI RMF evidence report: %s\n\n", skillDir)
	b.WriteString(disclaimer)
	b.WriteString("\n\n")

	b.WriteString("## MEASURE 2.1 — \"Test sets, metrics, and details about the tools used during TEVV are documented.\"\n\n")
	fmt.Fprintf(&b, "- %d eval case(s) in `evals/*.yaml`, each declaring its own assertions (the test set + metrics NIST asks to be documented).\n", ev.TotalCases)
	fmt.Fprintf(&b, "- %d case(s) have an explicit `triggered` assertion (trigger-accuracy test).\n\n", ev.CasesWithTriggered)

	b.WriteString("## MEASURE 2.3 — \"AI system performance or assurance criteria are measured qualitatively or quantitatively.\"\n\n")
	fmt.Fprintf(&b, "- %d case(s) use `judge:` criteria (%d total criteria) — qualitative, rubric-scored assessment by a separate model.\n", ev.CasesWithJudge, ev.TotalJudgeCriteria)
	fmt.Fprintf(&b, "- %d case(s) set a cost/latency/token budget assertion — quantitative performance measurement.\n\n", ev.CasesWithBudget)

	b.WriteString("## MEASURE 2.6 — \"The AI system is evaluated regularly for safety risks.\"\n\n")
	if ev.HasRuns {
		fmt.Fprintf(&b, "- %d recorded run(s) in `.skillci/history.json`, spanning %s to %s — evidence of regular (not one-time) evaluation.\n\n", ev.TotalRuns, ev.OldestRun.Format("2006-01-02"), ev.NewestRun.Format("2006-01-02"))
	} else {
		b.WriteString("- No recorded runs yet — run `skillci regress` to start building this evidence.\n\n")
	}

	b.WriteString("## MEASURE 2.7 — \"AI system security and resilience are evaluated and documented.\"\n\n")
	if ev.CasesWithRedteam > 0 {
		fmt.Fprintf(&b, "- %d case(s) run `redteam:` adversarial attack plugins: %s.\n\n", ev.CasesWithRedteam, strings.Join(ev.RedteamPlugins, ", "))
	} else {
		b.WriteString("- No `redteam:` assertions configured yet — see the main README for available plugins.\n\n")
	}

	b.WriteString("## MEASURE 2.13 — \"Effectiveness of the employed TEVV metrics and processes... are evaluated and documented.\"\n\n")
	fmt.Fprintf(&b, "- %d case(s) use `fuzz`/`fuzz_llm` paraphrase mutation — testing whether the trigger-accuracy check itself (MEASURE 2.1) holds up under rewording, not just the skill's behavior.\n", ev.CasesWithFuzz)
	fmt.Fprintf(&b, "- %d case(s) use `snapshot` golden-response comparison — detecting when a model update silently changes behavior a case's other assertions don't catch.\n", ev.CasesWithSnapshot)
	if len(ev.ModelsCovered) > 0 {
		fmt.Fprintf(&b, "- Recorded runs cover %d model(s): %s.\n", len(ev.ModelsCovered), strings.Join(ev.ModelsCovered, ", "))
	}

	return b.String()
}

// RenderEUAIAct maps Evidence onto EU AI Act articles. Article text below
// is quoted/paraphrased from the Act's own text via
// artificialintelligenceact.eu, verified against that primary source
// rather than a secondary summary.
func RenderEUAIAct(ev Evidence, skillDir string, historyRetentionRuns int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# EU AI Act evidence report: %s\n\n", skillDir)
	b.WriteString(disclaimer)
	b.WriteString("\n\n")
	b.WriteString("> This framework applies to **high-risk AI systems** as the Act defines them — check whether that classification actually applies to your use case before treating any of this as relevant compliance evidence.\n\n")

	b.WriteString("## Article 11 — Technical Documentation\n\n")
	b.WriteString("> \"The technical documentation of a high-risk AI system shall be drawn up before that system is placed on the market or put into service and shall be kept up-to date.\"\n\n")
	fmt.Fprintf(&b, "- `evals/*.yaml` (%d case(s)) and `SKILL.md` together document the system's intended behavior and how it's tested — a starting point for Annex IV's required content, not a complete substitute for it.\n\n", ev.TotalCases)

	b.WriteString("## Article 12 — Record-Keeping\n\n")
	b.WriteString("> \"High-risk AI systems shall technically allow for the automatic recording of events (logs) over the lifetime of the system.\"\n\n")
	if ev.HasRuns {
		fmt.Fprintf(&b, "- `.skillci/history.json` automatically records %d run(s) (%s to %s) with no manual step required — satisfying the *automatic* part of Article 12's requirement.\n\n", ev.TotalRuns, ev.OldestRun.Format("2006-01-02"), ev.NewestRun.Format("2006-01-02"))
	} else {
		b.WriteString("- No recorded runs yet — run `skillci regress` to start building this log.\n\n")
	}

	b.WriteString("## Articles 19 / 26(6) — Log Retention (\"at least six months\")\n\n")
	b.WriteString("> Providers (Article 19) and deployers (Article 26(6)) must each keep automatically generated logs \"for a period appropriate to the intended purpose of the high-risk AI system, of at least six months,\" unless other EU or national law requires longer.\n\n")
	effectiveCap := historyRetentionRuns
	if effectiveCap <= 0 {
		effectiveCap = history.DefaultMaxRetainedRuns
	}
	fmt.Fprintf(&b, "- `.skillci/history.json` retains the most recent %d run(s) (`history_retention_runs` in `.skillci.yaml`, default %d) — a **run-count cap, not a wall-clock one**.\n", effectiveCap, history.DefaultMaxRetainedRuns)
	b.WriteString("- Whether that count actually spans six months depends entirely on this skill's own CI cadence: at roughly one run per day, ")
	fmt.Fprintf(&b, "%d runs covers about %.1f months; ", effectiveCap, float64(effectiveCap)/30.4)
	b.WriteString("a higher-frequency pipeline needs a proportionally higher `history_retention_runs` to genuinely satisfy this window. skillci does not currently offer time-based (as opposed to count-based) retention — verify your own cadence against this requirement rather than assuming the default is sufficient.\n\n")

	b.WriteString("## Article 15 — Accuracy, Robustness and Cybersecurity\n\n")
	b.WriteString("> High-risk AI systems shall be resilient \"as regards errors, faults or inconsistencies... and as regards attempts by unauthorised third parties to alter their use or performance by exploiting the system vulnerabilities.\"\n\n")
	if ev.CasesWithRedteam > 0 {
		fmt.Fprintf(&b, "- %d case(s) run `redteam:` adversarial attack plugins: %s.\n\n", ev.CasesWithRedteam, strings.Join(ev.RedteamPlugins, ", "))
	} else {
		b.WriteString("- No `redteam:` assertions configured yet — see the main README for available plugins.\n\n")
	}

	return b.String()
}
