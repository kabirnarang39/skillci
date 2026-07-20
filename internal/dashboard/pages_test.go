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

// TestRenderSparklineWidthFitsAllPoints proves the SVG canvas is wide enough
// to actually display every point, not just the first ~10. Regression test
// for a fixed width="200" canvas silently clipping the most recent (and most
// important) results once a skill accumulated more than 10 history rows.
func TestRenderSparklineWidthFitsAllPoints(t *testing.T) {
	const n = 27 // more than the old fixed-width canvas could fit (10)
	results := make([]IngestedResult, n)
	for i := range results {
		results[i] = IngestedResult{Passed: i%2 == 0, Timestamp: time.Now().Add(time.Duration(i) * time.Hour)}
	}
	svg := RenderSparkline(results)

	widthRegex := regexp.MustCompile(`<svg[^>]*\bwidth="(\d+)"`)
	widthMatch := widthRegex.FindStringSubmatch(svg)
	if widthMatch == nil {
		t.Fatalf("no width attribute found in SVG: %s", svg)
	}
	svgWidth, err := strconv.Atoi(widthMatch[1])
	if err != nil {
		t.Fatalf("width attribute %q not an integer: %v", widthMatch[1], err)
	}

	cxRegex := regexp.MustCompile(`<circle cx="(\d+)"`)
	cxMatches := cxRegex.FindAllStringSubmatch(svg, -1)
	if len(cxMatches) != n {
		t.Fatalf("expected %d circles, found %d in SVG: %s", n, len(cxMatches), svg)
	}
	lastCx, err := strconv.Atoi(cxMatches[len(cxMatches)-1][1])
	if err != nil {
		t.Fatalf("last cx %q not an integer: %v", cxMatches[len(cxMatches)-1][1], err)
	}

	const radius = 3
	if svgWidth < lastCx+radius {
		t.Errorf("SVG width = %d, too small to contain last circle at cx=%d (radius %d); "+
			"the most recent result is clipped off-canvas", svgWidth, lastCx, radius)
	}
}

// TestSkillPageSparklineChronologicalOrder proves the skill page renders its
// sparkline oldest-to-newest. The x-coordinate of each circle is purely
// index-derived (see RenderSparkline: x := 10 + i*pointGap), so asserting on
// cx tells us nothing about ordering — cx is ascending for ANY input slice
// regardless of whether it's chronological. fill color, by contrast, depends
// on each result's Passed value, so the SEQUENCE of fill colors left-to-right
// genuinely reflects the order the handler fed results into RenderSparkline.
//
// Results are oldest=pass, middle=fail, newest=fail, giving expected
// left-to-right colors [green, red, red]. Because oldest and newest have
// different Passed values, this sequence is not a palindrome: if the
// skillPageHandler regressed to passing store rows (newest-to-oldest)
// straight into RenderSparkline instead of the chronologically-reversed
// copy, the observed sequence would be [red, red, green] — the reverse —
// and this test would fail.
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
	// Insert results with distinct timestamps AND distinct Passed values so
	// the rendered color sequence is content-identifiable, not just an
	// artifact of insertion count.
	results := []IngestedResult{
		{Owner: "test", Repo: "chronotest", Skill: skill, CommitSHA: "old", Model: "m1", Passed: true, Timestamp: baseTime.Add(-2 * time.Hour)},
		{Owner: "test", Repo: "chronotest", Skill: skill, CommitSHA: "mid", Model: "m1", Passed: false, Timestamp: baseTime.Add(-1 * time.Hour)},
		{Owner: "test", Repo: "chronotest", Skill: skill, CommitSHA: "new", Model: "m1", Passed: false, Timestamp: baseTime},
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

	// Extract fill values from circles in the SVG in document order (which is
	// ascending cx / index order — that part of RenderSparkline is unchanged
	// and correct, so document order reliably reflects the order the handler
	// passed results in).
	fillRegex := regexp.MustCompile(`fill="(#[0-9a-fA-F]+)"`)
	matches := fillRegex.FindAllStringSubmatch(svgContent, -1)
	if len(matches) != 3 {
		t.Fatalf("expected 3 circles in sparkline, found %d in SVG: %s", len(matches), svgContent)
	}

	const green, red = "#2ea44f", "#cf222e"
	want := []string{green, red, red} // oldest(pass), middle(fail), newest(fail)
	got := make([]string, len(matches))
	for i, match := range matches {
		got[i] = match[1]
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sparkline fill sequence = %v, want %v (chronological oldest-to-newest); "+
				"this order would invert to %v if the handler regressed to feeding newest-to-oldest rows into RenderSparkline",
				got, want, []string{red, red, green})
			break
		}
	}
}
