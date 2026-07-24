package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// maxIngestBodyBytes bounds how much of a request body ingestHandler will
// read before giving up — a small JSON payload (owner/repo/skill/commit/
// model/pass plus a handful of dimension entries) never needs anywhere
// close to this; it exists purely to cap worst-case memory from an
// oversized or malicious body, decoded before any other validation runs.
const maxIngestBodyBytes = 1 << 20 // 1 MiB

// TokenScope binds a bearer token to the exact owner/repo it may post
// results for. Owner/Repo empty means unscoped — authorized for any
// owner/repo, which is what a single self-hosted instance serving one
// project wants (and preserves the original single-shared-token
// behavior). A non-empty Owner/Repo restricts that token to only ever
// authorize payloads claiming that exact owner/repo, so a leaked token
// can't forge results for a different project sharing the same instance.
type TokenScope struct {
	Token string
	Owner string
	Repo  string
}

type DimensionEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Passed bool   `json:"passed"`
}

type IngestPayload struct {
	Owner      string           `json:"repo_owner"`
	Repo       string           `json:"repo"`
	Skill      string           `json:"skill_name"`
	CommitSHA  string           `json:"commit_sha"`
	Model      string           `json:"model"`
	Passed     bool             `json:"pass"`
	Dimensions []DimensionEntry `json:"dimensions,omitempty"`
}

func ingestHandler(store *Store, tokens []TokenScope) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxIngestBodyBytes)

		presented, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var scope *TokenScope
		for i := range tokens {
			if tokens[i].Token == presented {
				scope = &tokens[i]
				break
			}
		}
		if scope == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var p IngestPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "malformed JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(p.Owner) == "" || strings.TrimSpace(p.Repo) == "" || strings.TrimSpace(p.Skill) == "" {
			http.Error(w, "owner, repo, and skill_name are required", http.StatusBadRequest)
			return
		}
		if (scope.Owner != "" && scope.Owner != p.Owner) || (scope.Repo != "" && scope.Repo != p.Repo) {
			http.Error(w, "token is not authorized for this owner/repo", http.StatusForbidden)
			return
		}

		now := time.Now()
		err := store.InsertResult(r.Context(), IngestedResult{
			Owner: p.Owner, Repo: p.Repo, Skill: p.Skill,
			CommitSHA: p.CommitSHA, Model: p.Model, Passed: p.Passed,
			Timestamp: now,
		})
		if err != nil {
			http.Error(w, "failed to store result", http.StatusInternalServerError)
			return
		}

		for _, d := range p.Dimensions {
			err := store.InsertDimensionResult(r.Context(), DimensionResult{
				Owner: p.Owner, Repo: p.Repo, Skill: p.Skill,
				CommitSHA: p.CommitSHA, Model: p.Model,
				DimensionKey: d.Key, DimensionValue: d.Value, Passed: d.Passed,
				Timestamp: now,
			})
			if err != nil {
				http.Error(w, "failed to store dimension result", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusCreated)
	}
}
