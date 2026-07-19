package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func requireTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set, skipping Postgres-backed test")
	}
	s, err := NewStore(url)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return s
}

func TestIngestHandlerAcceptsValidPayload(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, "secret-token")

	payload := IngestPayload{
		Owner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestIngestHandlerRejectsMissingAuth(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, "secret-token")

	body, _ := json.Marshal(IngestPayload{Owner: "o", Repo: "r", Skill: "s", CommitSHA: "c", Model: "m", Passed: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIngestHandlerRejectsMalformedJSON(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, "secret-token")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
