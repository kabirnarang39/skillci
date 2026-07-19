package badge

import (
	"fmt"

	"github.com/kabirnarang/skillci/internal/history"
)

type State string

const (
	Passing   State = "passing"
	Partial   State = "partial"
	Regressed State = "regressed"
)

func StateFromRun(run history.Run) State {
	if len(run.Cases) == 0 {
		return Regressed
	}
	passCount := 0
	for _, c := range run.Cases {
		if c.Passed {
			passCount++
		}
	}
	switch {
	case passCount == len(run.Cases):
		return Passing
	case passCount == 0:
		return Regressed
	default:
		return Partial
	}
}

func color(s State) string {
	switch s {
	case Passing:
		return "#2ea44f"
	case Partial:
		return "#dbab09"
	default:
		return "#cf222e"
	}
}

// Render returns a shields.io-style flat SVG badge for the given state.
// Committed directly to the repo by the GitHub Action — no external image
// host, so no hotlink-availability failure mode.
func Render(state State) string {
	c := color(state)
	label := string(state)
	width := 58 + len(label)*7
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="skillci: %s">
  <rect width="58" height="20" fill="#555"/>
  <rect x="58" width="%d" height="20" fill="%s"/>
  <text x="6" y="14" fill="#fff" font-family="Verdana,sans-serif" font-size="11">skillci</text>
  <text x="65" y="14" fill="#fff" font-family="Verdana,sans-serif" font-size="11">%s</text>
</svg>`, width, label, width-58, c, label)
}
