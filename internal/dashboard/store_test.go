package dashboard

import (
	"context"
	"os"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
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

func TestInsertAndFetchSkillHistory(t *testing.T) {
	s := testStore(t)
	r := IngestedResult{
		Owner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
		Timestamp: time.Now(),
	}
	if err := s.InsertResult(context.Background(), r); err != nil {
		t.Fatalf("InsertResult() error = %v", err)
	}

	history, err := s.SkillHistory(context.Background(), "kabirnarang", "skillci", "pr-review")
	if err != nil {
		t.Fatalf("SkillHistory() error = %v", err)
	}
	if len(history) == 0 {
		t.Error("SkillHistory() returned no rows after insert")
	}
}

func TestLeaderboard(t *testing.T) {
	s := testStore(t)
	r := IngestedResult{
		Owner: "kabirnarang", Repo: "skillci", Skill: "leaderboard-case",
		CommitSHA: "def456", Model: "claude-sonnet-5", Passed: true,
		Timestamp: time.Now(),
	}
	if err := s.InsertResult(context.Background(), r); err != nil {
		t.Fatalf("InsertResult() error = %v", err)
	}

	entries, err := s.Leaderboard(context.Background())
	if err != nil {
		t.Fatalf("Leaderboard() error = %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Skill == "leaderboard-case" {
			found = true
			if e.PassRate != 1.0 {
				t.Errorf("PassRate = %v, want 1.0", e.PassRate)
			}
		}
	}
	if !found {
		t.Error("Leaderboard() did not include the inserted skill")
	}
}
