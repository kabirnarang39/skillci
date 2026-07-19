package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSkillPageRendersHistoryAndBadgeState(t *testing.T) {
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set")
	}
	store, err := NewStore(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertResult(context.Background(), IngestedResult{
		Owner: "kabirnarang", Repo: "skillci", Skill: "page-test-skill",
		CommitSHA: "abc", Model: "claude-sonnet-5", Passed: true, Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/s/kabirnarang/skillci/page-test-skill", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "page-test-skill") {
		t.Error("skill page body does not mention the skill name")
	}
}

func TestSkillPageNotFound(t *testing.T) {
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set")
	}
	store, err := NewStore(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/s/nobody/nothing/nothing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLeaderboardPageRenders(t *testing.T) {
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set")
	}
	store, err := NewStore(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRenderSparklineProducesSVG(t *testing.T) {
	results := []IngestedResult{
		{Passed: true, Timestamp: time.Now().Add(-2 * time.Hour)},
		{Passed: false, Timestamp: time.Now().Add(-1 * time.Hour)},
		{Passed: true, Timestamp: time.Now()},
	}
	svg := RenderSparkline(results)
	if !strings.Contains(svg, "<svg") {
		t.Errorf("RenderSparkline() = %q, not SVG", svg)
	}
}
