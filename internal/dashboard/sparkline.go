package dashboard

import (
	"fmt"
	"strings"
)

// RenderSparkline draws a minimal pass/fail trend line: one point per
// result, green for pass, red for fail, left-to-right oldest-to-newest.
// Deliberately hand-rolled SVG (no charting library) — same approach as
// internal/badge, keeps both binaries dependency-light.
func RenderSparkline(results []IngestedResult) string {
	const width, height, pointGap = 200, 40, 20
	var points strings.Builder
	for i, r := range results {
		x := 10 + i*pointGap
		y := 30
		if r.Passed {
			y = 10
		}
		color := "#cf222e"
		if r.Passed {
			color = "#2ea44f"
		}
		fmt.Fprintf(&points, `<circle cx="%d" cy="%d" r="3" fill="%s"/>`, x, y, color)
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">%s</svg>`, width, height, points.String())
}
