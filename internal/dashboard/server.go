package dashboard

import "net/http"

// NewServer wires the dashboard's HTTP routes. Task 18 (public skill pages
// and the leaderboard) registers additional GET routes on the same mux
// returned here — do not construct a second mux for those routes.
func NewServer(store *Store, ingestToken string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/results", ingestHandler(store, ingestToken))
	return mux
}
