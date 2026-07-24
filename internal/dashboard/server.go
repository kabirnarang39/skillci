package dashboard

import "net/http"

// NewServer wires the dashboard's HTTP routes: the results ingestion API,
// the public per-skill history page, and the root leaderboard page.
func NewServer(store *Store, tokens []TokenScope) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/results", ingestHandler(store, tokens))
	mux.HandleFunc("GET /s/{owner}/{repo}/{skill}", skillPageHandler(store))
	mux.HandleFunc("GET /{$}", leaderboardHandler(store))
	return mux
}
