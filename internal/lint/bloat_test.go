// internal/lint/bloat_test.go
package lint

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestBloatReferencedFileCountIssueFiresOverBudget(t *testing.T) {
	matches := make([]string, 11)
	for i := range matches {
		matches[i] = fmt.Sprintf("scripts/file%d.py", i)
	}
	iss := bloatReferencedFileCountIssue("f.md", matches)
	if iss == nil {
		t.Fatal("bloatReferencedFileCountIssue() = nil, want an issue for 11 distinct referenced files")
	}
	if iss.Rule != "bloat-referenced-file-count" {
		t.Errorf("Rule = %q, want bloat-referenced-file-count", iss.Rule)
	}
}

func TestBloatReferencedFileCountIssueNoIssueUnderBudget(t *testing.T) {
	matches := make([]string, 10)
	for i := range matches {
		matches[i] = fmt.Sprintf("scripts/file%d.py", i)
	}
	if iss := bloatReferencedFileCountIssue("f.md", matches); iss != nil {
		t.Errorf("bloatReferencedFileCountIssue() = %+v, want nil for exactly 10 (at-budget) distinct files", iss)
	}
}

func TestBloatReferencedFileCountIssueDeduplicatesMatches(t *testing.T) {
	// The same path mentioned 15 times in prose is still just 1 distinct
	// file — must not fire the >10 rule.
	matches := make([]string, 15)
	for i := range matches {
		matches[i] = "scripts/helper.py"
	}
	if iss := bloatReferencedFileCountIssue("f.md", matches); iss != nil {
		t.Errorf("bloatReferencedFileCountIssue() = %+v, want nil — only 1 distinct file despite 15 mentions", iss)
	}
}

func TestBloatReferencedFileSizeIssueFiresOverBudget(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 100*1024+1)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	iss := bloatReferencedFileSizeIssue("f.md", dir, []string{"big.txt"})
	if iss == nil {
		t.Fatal("bloatReferencedFileSizeIssue() = nil, want an issue for a 100KB+1-byte referenced file")
	}
	if iss.Rule != "bloat-referenced-file-size" {
		t.Errorf("Rule = %q, want bloat-referenced-file-size", iss.Rule)
	}
}

func TestBloatReferencedFileSizeIssueNoIssueUnderBudget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if iss := bloatReferencedFileSizeIssue("f.md", dir, []string{"small.txt"}); iss != nil {
		t.Errorf("bloatReferencedFileSizeIssue() = %+v, want nil for a tiny referenced file", iss)
	}
}

func TestBloatReferencedFileSizeIssueSkipsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if iss := bloatReferencedFileSizeIssue("f.md", dir, []string{"does-not-exist.txt"}); iss != nil {
		t.Errorf("bloatReferencedFileSizeIssue() = %+v, want nil — a missing file contributes 0 to the total, doesn't error", iss)
	}
}

func TestBloatReferencedFileSizeIssueDeduplicatesMatches(t *testing.T) {
	// Same file mentioned twice in matches must only be sized once.
	dir := t.TempDir()
	big := make([]byte, 60*1024)
	if err := os.WriteFile(filepath.Join(dir, "medium.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	// 60KB counted once = under 100KB budget; counted twice (120KB) would
	// incorrectly exceed it.
	if iss := bloatReferencedFileSizeIssue("f.md", dir, []string{"medium.txt", "medium.txt"}); iss != nil {
		t.Errorf("bloatReferencedFileSizeIssue() = %+v, want nil — the file must be sized once despite appearing twice in matches", iss)
	}
}
