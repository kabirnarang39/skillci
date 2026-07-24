package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

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

func ingestHandler(store *Store, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
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
