// internal/runner/runner_test.go
package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/evalspec"
)

func newSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: pr-review\ndescription: Reviews pull requests for SOLID violations.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubServer(t *testing.T, replyText string, inputTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": inputTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func truePtr() *bool    { v := true; return &v }
func intPtr(v int) *int { return &v }

func TestRunCasePassing(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nThis review found a SOLID violation. Overall verdict: REQUEST_CHANGES.", 500)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:           "triggers-on-pr-review-request",
		Prompt:         "Can you review this PR for SOLID violations?",
		SkillUnderTest: "pr-review",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			Contains:        []string{"SOLID", "verdict"},
			NotContains:     []string{"I cannot"},
			MaxTokensLoaded: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Triggered {
		t.Error("Triggered = false, want true")
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsOnMissingContains(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nLooks fine to me.", 500)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Contains:  []string{"SOLID"},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (missing required substring)")
	}
}

func TestRunCaseFailsOnUnexpectedTrigger(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: false", 200)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	falsePtr := func() *bool { v := false; return &v }
	c := evalspec.Case{
		Name:   "should-not-trigger",
		Prompt: "what's the weather",
		Assert: evalspec.Assertions{Triggered: falsePtr()},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true (correctly did not trigger); Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsOnTokenBudget(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nSOLID verdict here.", 5000)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "budget-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			MaxTokensLoaded: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (5000 tokens exceeds 3000 budget)")
	}
}
