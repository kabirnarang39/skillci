package dashboard

import (
	"fmt"
	"html/template"
	"net/http"
)

var skillPageTmpl = template.Must(template.New("skill").Parse(`<!doctype html>
<html><head><title>{{.Owner}}/{{.Repo}} — {{.Skill}}</title></head>
<body>
<h1>{{.Skill}}</h1>
<p>{{.Owner}}/{{.Repo}}</p>
{{.SparklineSVG}}
<table>
<tr><th>Model</th><th>Commit</th><th>Result</th><th>When</th></tr>
{{range .Rows}}<tr><td>{{.Model}}</td><td>{{.CommitSHA}}</td><td>{{if .Passed}}pass{{else}}fail{{end}}</td><td>{{.Timestamp}}</td></tr>
{{end}}
</table>
</body></html>`))

var leaderboardTmpl = template.Must(template.New("leaderboard").Parse(`<!doctype html>
<html><head><title>SkillCI Leaderboard</title></head>
<body>
<h1>SkillCI Leaderboard</h1>
<table>
<tr><th>Skill</th><th>Repo</th><th>Pass Rate</th><th>Models</th><th>Last Run</th></tr>
{{range .}}<tr><td><a href="/s/{{.Owner}}/{{.Repo}}/{{.Skill}}">{{.Skill}}</a></td><td>{{.Owner}}/{{.Repo}}</td><td>{{.PassRate}}</td><td>{{.ModelsCovered}}</td><td>{{.LastRun}}</td></tr>
{{end}}
</table>
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
