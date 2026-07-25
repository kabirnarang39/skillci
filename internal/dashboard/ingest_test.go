package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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
	mux := NewServer(store, []TokenScope{{Token: "secret-token"}})

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
	mux := NewServer(store, []TokenScope{{Token: "secret-token"}})

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
	mux := NewServer(store, []TokenScope{{Token: "secret-token"}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestIngestHandlerRejectsOversizedBody(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, []TokenScope{{Token: "secret-token"}})

	// One byte over the cap — a valid JSON shape doesn't matter here, since
	// MaxBytesReader trips during Decode's read before Decode can ever
	// finish parsing, regardless of what the bytes actually contain.
	oversized := bytes.Repeat([]byte("a"), maxIngestBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(oversized))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (oversized body must be rejected, not read fully into memory)", rec.Code, http.StatusBadRequest)
	}
}

func TestIngestHandlerRejectsTokenNotAuthorizedForOwnerRepo(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, []TokenScope{{Token: "scoped-token", Owner: "myorg", Repo: "allowed-repo"}})

	payload := IngestPayload{
		Owner: "myorg", Repo: "different-repo", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d — a scoped token must not authorize a different repo, even under the same owner", rec.Code, http.StatusForbidden)
	}
}

func TestIngestHandlerAcceptsTokenAuthorizedForMatchingOwnerRepo(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, []TokenScope{{Token: "scoped-token", Owner: "myorg", Repo: "allowed-repo"}})

	payload := IngestPayload{
		Owner: "myorg", Repo: "allowed-repo", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestIngestHandlerMultipleScopedTokensDoNotCrossAuthorize(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, []TokenScope{
		{Token: "token-a", Owner: "org-a", Repo: "repo-a"},
		{Token: "token-b", Owner: "org-b", Repo: "repo-b"},
	})

	// token-a presented, but the payload claims org-b/repo-b — must be
	// rejected even though token-b (a DIFFERENT valid token on this same
	// instance) is authorized for exactly that owner/repo.
	payload := IngestPayload{
		Owner: "org-b", Repo: "repo-b", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-a")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d — token-a must not authorize org-b/repo-b just because token-b is valid for it", rec.Code, http.StatusForbidden)
	}
}

func TestIngestHandlerStoresDimensionEntries(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, []TokenScope{{Token: "secret-token"}})

	body := `{
		"repo_owner": "kabirnarang", "repo": "skillci", "skill_name": "dim-ingest-skill",
		"commit_sha": "abc123", "model": "claude-sonnet-5", "pass": false,
		"dimensions": [
			{"key": "segment", "value": "enterprise", "passed": false},
			{"key": "language", "value": "es", "passed": true}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}

	rows, err := store.LatestDimensionResults(context.Background(), "kabirnarang", "skillci", "dim-ingest-skill")
	if err != nil {
		t.Fatalf("LatestDimensionResults() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("LatestDimensionResults() = %v, want 2 rows (segment and language)", rows)
	}
}

// TestIngestHandlerConcurrentRequestsAllSucceed fires real concurrent HTTP
// requests at the handler (via a real net/http server, not
// httptest.NewRecorder — that's single-goroutine by construction and would
// never exercise the handler running on genuinely parallel goroutines the
// way a real deployment does). Previously untested: nothing in this
// package had ever run more than one request through the handler at once.
// Every request uses a distinct commit SHA so a lost or corrupted write is
// individually detectable, not just a row-count mismatch.
func TestIngestHandlerConcurrentRequestsAllSucceed(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, []TokenScope{{Token: "secret-token"}})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	const n = 50
	skill := fmt.Sprintf("concurrent-skill-%d", time.Now().UnixNano())

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := IngestPayload{
				Owner: "concurrent-owner", Repo: "concurrent-repo", Skill: skill,
				CommitSHA: fmt.Sprintf("sha-%d", i), Model: "claude-sonnet-5", Passed: true,
			}
			body, _ := json.Marshal(payload)
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/results", bytes.NewReader(body))
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Authorization", "Bearer secret-token")
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				errs <- fmt.Errorf("request %d: status = %d, want 201", i, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	rows, err := store.SkillHistory(context.Background(), "concurrent-owner", "concurrent-repo", skill)
	if err != nil {
		t.Fatalf("SkillHistory() error = %v", err)
	}
	if len(rows) != n {
		t.Errorf("SkillHistory() = %d rows, want %d — a concurrent write was lost or corrupted", len(rows), n)
	}
	seen := make(map[string]bool, n)
	for _, r := range rows {
		if seen[r.CommitSHA] {
			t.Errorf("duplicate commit_sha %q in results — a row was written twice", r.CommitSHA)
		}
		seen[r.CommitSHA] = true
	}
}
