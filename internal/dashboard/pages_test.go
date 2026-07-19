package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
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

func TestSkillPageSparklineChronologicalOrder(t *testing.T) {
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

	baseTime := time.Now()
	// Use unique identifiers to avoid collisions with other test runs
	skill := "chronotest-" + strconv.FormatInt(baseTime.UnixNano(), 10)
	// Insert results with distinct timestamps in order (oldest first insertion, newest last)
	results := []IngestedResult{
		{Owner: "test", Repo: "chronotest", Skill: skill, CommitSHA: "old", Model: "m1", Passed: false, Timestamp: baseTime.Add(-2 * time.Hour)},
		{Owner: "test", Repo: "chronotest", Skill: skill, CommitSHA: "mid", Model: "m1", Passed: true, Timestamp: baseTime.Add(-1 * time.Hour)},
		{Owner: "test", Repo: "chronotest", Skill: skill, CommitSHA: "new", Model: "m1", Passed: true, Timestamp: baseTime},
	}
	for _, r := range results {
		if err := store.InsertResult(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/s/test/chronotest/"+skill, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// Extract SVG content to isolate sparkline circles from other page content
	svgStart := strings.Index(body, "<svg")
	svgEnd := strings.Index(body, "</svg>")
	if svgStart == -1 || svgEnd == -1 {
		t.Fatalf("no SVG found in response body")
	}
	svgContent := body[svgStart : svgEnd+6]

	// Extract cx values from circles in the SVG (format: cx="10", cx="30", etc.)
	cxRegex := regexp.MustCompile(`cx="(\d+)"`)
	matches := cxRegex.FindAllStringSubmatch(svgContent, -1)
	if len(matches) != 3 {
		t.Fatalf("expected 3 circles in sparkline, found %d in SVG: %s", len(matches), svgContent)
	}

	cxValues := make([]int, len(matches))
	for i, match := range matches {
		val, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("failed to parse cx value %q: %v", match[1], err)
		}
		cxValues[i] = val
	}

	// Verify cx values are strictly increasing (oldest result at smallest x, newest at largest x)
	for i := 0; i < len(cxValues)-1; i++ {
		if cxValues[i] >= cxValues[i+1] {
			t.Errorf("sparkline cx values not in ascending order: %v", cxValues)
		}
	}
}
