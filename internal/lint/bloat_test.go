// internal/lint/bloat_test.go
package lint

import (
	"strings"
	"testing"
)

func TestBloatBodyLengthIssueFiresOverBudget(t *testing.T) {
	body := strings.Repeat("a", 8001)
	iss := bloatBodyLengthIssue("f.md", body)
	if iss == nil {
		t.Fatal("bloatBodyLengthIssue() = nil, want an issue for an 8001-char body")
	}
	if iss.Rule != "bloat-body-length" {
		t.Errorf("Rule = %q, want bloat-body-length", iss.Rule)
	}
}

func TestBloatBodyLengthIssueNoIssueUnderBudget(t *testing.T) {
	body := strings.Repeat("a", 8000)
	if iss := bloatBodyLengthIssue("f.md", body); iss != nil {
		t.Errorf("bloatBodyLengthIssue() = %+v, want nil for an 8000-char (at-budget) body", iss)
	}
}

func TestBloatDuplicateLineIssuesFindsExactDuplicate(t *testing.T) {
	body := "First unique instruction line here.\nSome other line.\nFirst unique instruction line here.\n"
	issues := bloatDuplicateLineIssues("f.md", body)
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Rule != "bloat-duplicate-line" {
		t.Errorf("Rule = %q, want bloat-duplicate-line", issues[0].Rule)
	}
	if issues[0].Line != 3 {
		t.Errorf("Line = %d, want 3 (the second occurrence)", issues[0].Line)
	}
}

func TestBloatDuplicateLineIssuesIgnoresShortLines(t *testing.T) {
	body := "- OK\nSome other line.\n- OK\n"
	issues := bloatDuplicateLineIssues("f.md", body)
	if len(issues) != 0 {
		t.Errorf("issues = %+v, want none — \"- OK\" is only 4 chars, under the 20-char non-triviality threshold", issues)
	}
}

func TestBloatDuplicateLineIssuesIgnoresWhitespaceOnlyDifference(t *testing.T) {
	// Both lines are identical once trimmed — still a duplicate, since the
	// design compares AFTER TrimSpace.
	body := "This is a long enough instruction line.\nSome other line.\n   This is a long enough instruction line.   \n"
	issues := bloatDuplicateLineIssues("f.md", body)
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1 (trimmed lines are identical)", len(issues))
	}
}

func TestBloatDuplicateLineIssuesNoFalsePositiveOnDistinctLines(t *testing.T) {
	body := "This is the first distinct instruction line.\nThis is a second, different instruction line.\n"
	issues := bloatDuplicateLineIssues("f.md", body)
	if len(issues) != 0 {
		t.Errorf("issues = %+v, want none for two genuinely distinct lines", issues)
	}
}
