package dashboard

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"
)

// commonStyle is shared by every dashboard page: design tokens (dark,
// developer-tool aesthetic — deep surface, indigo brand accent, semantic
// green/red for pass/fail), component styles, and motion. Kept as one block
// so both templates stay visually identical without duplicating rules.
const commonStyle = `
<style>
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap');

:root {
  --bg: #050506;
  --bg-elevated: #0c0c0f;
  --bg-elevated-hover: #131318;
  --border: rgba(255,255,255,0.08);
  --border-hover: rgba(255,255,255,0.16);
  --fg: #F4F4F5;
  --fg-muted: #8A8F98;
  --fg-subtle: #5C5F66;
  --accent: #5E6AD2;
  --accent-fg: #EEF0FF;
  --accent-glow: rgba(94,106,210,0.35);
  --pass: #22C55E;
  --pass-bg: rgba(34,197,94,0.14);
  --fail: #F43F5E;
  --fail-bg: rgba(244,63,94,0.14);
  --radius: 12px;
  --radius-sm: 8px;
  --ease: cubic-bezier(0.16,1,0.3,1);
  --font-sans: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}

* { box-sizing: border-box; }

html, body {
  margin: 0;
  padding: 0;
  background: var(--bg);
  color: var(--fg);
  font-family: var(--font-sans);
  font-size: 15px;
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
}

body {
  background-image:
    radial-gradient(600px circle at 85% -10%, var(--accent-glow), transparent 60%),
    radial-gradient(500px circle at -10% 20%, rgba(34,197,94,0.08), transparent 55%);
  background-repeat: no-repeat;
  min-height: 100dvh;
}

a { color: inherit; text-decoration: none; }

::selection { background: var(--accent-glow); }

:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: var(--radius-sm);
}

.container {
  max-width: 960px;
  margin: 0 auto;
  padding: 0 24px 64px;
}

.topbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 28px 24px 0;
  max-width: 960px;
  margin: 0 auto;
}

.brand-mark {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--accent), #8B93E8);
  box-shadow: 0 0 24px var(--accent-glow);
  flex-shrink: 0;
}

.brand-name {
  font-weight: 700;
  font-size: 15px;
  letter-spacing: -0.01em;
}

.brand-name a:hover { color: var(--accent-fg); }

.hero {
  padding: 28px 0 32px;
}

.hero h1 {
  font-size: 28px;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0 0 6px;
}

.hero .sub {
  color: var(--fg-muted);
  font-size: 14px;
  margin: 0;
}

.hero .sub a { color: var(--fg-muted); border-bottom: 1px solid var(--border); transition: color 150ms var(--ease), border-color 150ms var(--ease); }
.hero .sub a:hover { color: var(--accent-fg); border-color: var(--accent); }

.stats-row {
  display: flex;
  gap: 12px;
  margin-bottom: 24px;
  flex-wrap: wrap;
}

.stat {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 14px 18px;
  min-width: 120px;
}

.stat-value {
  font-family: var(--font-mono);
  font-size: 22px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.stat-label {
  font-size: 12px;
  color: var(--fg-muted);
  margin-top: 2px;
}

.card {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
}

.card-header {
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.sparkline-wrap {
  padding: 20px 20px 8px;
  overflow-x: auto;
}

.sparkline-wrap svg { display: block; }

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

thead th {
  text-align: left;
  font-weight: 500;
  color: var(--fg-subtle);
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 12px 20px;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}

tbody td {
  padding: 14px 20px;
  border-bottom: 1px solid var(--border);
  vertical-align: middle;
}

tbody tr:last-child td { border-bottom: none; }

tbody tr {
  transition: background-color 150ms var(--ease);
}

tbody tr:hover {
  background: var(--bg-elevated-hover);
}

.mono {
  font-family: var(--font-mono);
  font-size: 13px;
  color: var(--fg-muted);
}

.skill-link {
  font-weight: 600;
  transition: color 150ms var(--ease);
  position: relative;
}

.skill-link:hover { color: var(--accent-fg); }

.pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  line-height: 1.4;
}

.pill-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.pill-pass { background: var(--pass-bg); color: var(--pass); }
.pill-pass .pill-dot { background: var(--pass); box-shadow: 0 0 6px var(--pass); }

.pill-fail { background: var(--fail-bg); color: var(--fail); }
.pill-fail .pill-dot { background: var(--fail); box-shadow: 0 0 6px var(--fail); }

.pill-neutral { background: rgba(255,255,255,0.06); color: var(--fg-muted); }

.empty-state {
  padding: 48px 20px;
  text-align: center;
  color: var(--fg-muted);
  font-size: 14px;
}

/* Entrance motion: fade + rise, staggered per row. One hero animation per
   view (the card), plus per-row stagger — kept subtle (opacity + transform
   only, GPU-cheap, no layout thrash). */
@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.card, .stats-row { animation: fadeInUp 420ms var(--ease) both; }

tbody tr {
  animation: fadeInUp 320ms var(--ease) both;
  animation-delay: var(--row-delay, 0ms);
}

@media (prefers-reduced-motion: reduce) {
  * { animation: none !important; transition: none !important; }
}

@media (max-width: 640px) {
  .container, .topbar, .hero { padding-left: 16px; padding-right: 16px; }
  table { font-size: 13px; }
  thead th, tbody td { padding: 10px 12px; }
}
</style>`

// pageFuncs are small formatting helpers html/template can't express inline.
var pageFuncs = template.FuncMap{
	"shortSHA": func(sha string) string {
		if len(sha) > 7 {
			return sha[:7]
		}
		return sha
	},
	"percent": func(rate float64) string {
		return strconv.Itoa(int(rate*100+0.5)) + "%"
	},
	"since": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		d := time.Since(t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		case d < 30*24*time.Hour:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		default:
			return t.Format("Jan 2, 2006")
		}
	},
	// stagger returns a CSS custom-property declaration for row-entrance
	// delay, capped so a long table doesn't take seconds to finish animating.
	"stagger": func(i int) template.CSS {
		delay := min(i*35, 350)
		return template.CSS(fmt.Sprintf("--row-delay:%dms", delay))
	},
}

var skillPageTmpl = template.Must(template.New("skill").Funcs(pageFuncs).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Owner}}/{{.Repo}} — {{.Skill}} · SkillCI</title>
` + commonStyle + `
</head>
<body>
<div class="topbar">
  <div class="brand-mark"></div>
  <div class="brand-name"><a href="/">SkillCI</a></div>
</div>
<div class="container">
  <div class="hero">
    <h1>{{.Skill}}</h1>
    <p class="sub"><a href="/">{{.Owner}}/{{.Repo}}</a></p>
  </div>

  <div class="card" style="margin-bottom: 20px;">
    <div class="card-header">History Trend</div>
    <div class="sparkline-wrap">{{.SparklineSVG}}</div>
  </div>

  <div class="card">
    <div class="card-header">Run History</div>
    <table>
      <thead><tr><th>Model</th><th>Commit</th><th>Result</th><th>When</th></tr></thead>
      <tbody>
      {{range $i, $row := .Rows}}<tr style="{{stagger $i}}">
        <td class="mono">{{$row.Model}}</td>
        <td class="mono">{{shortSHA $row.CommitSHA}}</td>
        <td>{{if $row.Passed}}<span class="pill pill-pass"><span class="pill-dot"></span>pass</span>{{else}}<span class="pill pill-fail"><span class="pill-dot"></span>fail</span>{{end}}</td>
        <td class="mono">{{since $row.Timestamp}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
  </div>
</div>
</body></html>`))

var leaderboardTmpl = template.Must(template.New("leaderboard").Funcs(pageFuncs).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>SkillCI Leaderboard</title>
` + commonStyle + `
</head>
<body>
<div class="topbar">
  <div class="brand-mark"></div>
  <div class="brand-name">SkillCI</div>
</div>
<div class="container">
  <div class="hero">
    <h1>Compatibility Leaderboard</h1>
    <p class="sub">Cross-model regression tracking for Claude Skills</p>
  </div>

  <div class="stats-row">
    <div class="stat">
      <div class="stat-value">{{len .}}</div>
      <div class="stat-label">Skills tracked</div>
    </div>
  </div>

  <div class="card">
    {{if not .}}
    <div class="empty-state">No skills reporting yet — run <span class="mono">skillci regress --upload</span> to appear here.</div>
    {{else}}
    <table>
      <thead><tr><th>Skill</th><th>Repo</th><th>Pass Rate</th><th>Models</th><th>Last Run</th></tr></thead>
      <tbody>
      {{range $i, $e := .}}<tr style="{{stagger $i}}">
        <td><a class="skill-link" href="/s/{{$e.Owner}}/{{$e.Repo}}/{{$e.Skill}}">{{$e.Skill}}</a></td>
        <td class="mono">{{$e.Owner}}/{{$e.Repo}}</td>
        <td>{{if eq $e.PassRate 1.0}}<span class="pill pill-pass"><span class="pill-dot"></span>{{percent $e.PassRate}}</span>{{else if eq $e.PassRate 0.0}}<span class="pill pill-fail"><span class="pill-dot"></span>{{percent $e.PassRate}}</span>{{else}}<span class="pill pill-neutral">{{percent $e.PassRate}}</span>{{end}}</td>
        <td class="mono">{{$e.ModelsCovered}}</td>
        <td class="mono">{{since $e.LastRun}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
    {{end}}
  </div>
</div>
</body></html>`))

type skillPageData struct {
	Owner, Repo, Skill string
	SparklineSVG       template.HTML
	Rows               []IngestedResult
}

func skillPageHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner := r.PathValue("owner")
		repo := r.PathValue("repo")
		skill := r.PathValue("skill")

		rows, err := store.SkillHistory(r.Context(), owner, repo, skill)
		if err != nil {
			http.Error(w, "failed to load history", http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			http.NotFound(w, r)
			return
		}

		// RenderSparkline expects oldest-to-newest; SkillHistory returns newest-to-oldest.
		// Reverse for sparkline while keeping rows in original order for template.
		sparklineRows := make([]IngestedResult, len(rows))
		copy(sparklineRows, rows)
		for i, j := 0, len(sparklineRows)-1; i < j; i, j = i+1, j-1 {
			sparklineRows[i], sparklineRows[j] = sparklineRows[j], sparklineRows[i]
		}

		data := skillPageData{
			Owner: owner, Repo: repo, Skill: skill,
			SparklineSVG: template.HTML(RenderSparkline(sparklineRows)),
			Rows:         rows,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := skillPageTmpl.Execute(w, data); err != nil {
			http.Error(w, fmt.Sprintf("render error: %v", err), http.StatusInternalServerError)
		}
	}
}

func leaderboardHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := store.Leaderboard(r.Context())
		if err != nil {
			http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := leaderboardTmpl.Execute(w, entries); err != nil {
			http.Error(w, fmt.Sprintf("render error: %v", err), http.StatusInternalServerError)
		}
	}
}
