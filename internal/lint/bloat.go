// Package lint's bloat.go implements static, non-LLM "skill bloat" checks:
// body length, exact-duplicate lines, referenced-file count, and
// referenced-file total size. These are warning-level rules answering a
// widely-echoed complaint that installed skills often add tokens/latency
// without proportional value — all local, no API calls, same as every
// other lint rule in this package.
package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxBodyLength = 8000

// bloatBodyLengthIssue fires when body exceeds the fixed character budget.
// Returns nil when body is at or under budget.
func bloatBodyLengthIssue(skillPath, body string) *Issue {
	if len(body) <= maxBodyLength {
		return nil
	}
	return &Issue{
		File: skillPath,
		Line: 1,
		Rule: "bloat-body-length",
		Msg:  fmt.Sprintf("SKILL.md body is %d characters, over the %d-character budget — every extra instruction is loaded on every invocation", len(body), maxBodyLength),
	}
}

const minNonTrivialLineLength = 20

// bloatDuplicateLineIssues finds exact-duplicate lines in body: for each
// line, trim whitespace, and if the trimmed result is longer than
// minNonTrivialLineLength and byte-identical to an earlier trimmed line,
// report the second (and any later) occurrence. Blank lines, single
// words, and short markdown bullets/headers are below the length
// threshold and never flagged, even if they repeat legitimately.
func bloatDuplicateLineIssues(skillPath, body string) []Issue {
	var issues []Issue
	seen := make(map[string]bool)
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) <= minNonTrivialLineLength {
			continue
		}
		if seen[trimmed] {
			issues = append(issues, Issue{
				File: skillPath,
				Line: i + 1,
				Rule: "bloat-duplicate-line",
				Msg:  fmt.Sprintf("line duplicates an earlier line: %q", trimmed),
			})
			continue
		}
		seen[trimmed] = true
	}
	return issues
}

const maxReferencedFileCount = 10

// bloatReferencedFileCountIssue fires when the number of DISTINCT paths in
// matches exceeds the fixed budget. matches may contain duplicates (the
// same file mentioned more than once in prose) — those are deduplicated
// before counting.
func bloatReferencedFileCountIssue(skillPath string, matches []string) *Issue {
	distinct := make(map[string]bool)
	for _, m := range matches {
		distinct[m] = true
	}
	if len(distinct) <= maxReferencedFileCount {
		return nil
	}
	return &Issue{
		File: skillPath,
		Line: 1,
		Rule: "bloat-referenced-file-count",
		Msg:  fmt.Sprintf("skill references %d distinct files, over the %d-file budget — more files means more surface area to audit", len(distinct), maxReferencedFileCount),
	}
}

const maxReferencedFileTotalBytes = 100 * 1024

// bloatReferencedFileSizeIssue fires when the combined byte size of every
// DISTINCT existing file in matches (relative to dir) exceeds the fixed
// budget. A path that doesn't exist on disk contributes 0 to the total —
// missing-referenced-file already reports it separately, and this
// function never errors on a missing file.
func bloatReferencedFileSizeIssue(skillPath, dir string, matches []string) *Issue {
	distinct := make(map[string]bool)
	var total int64
	for _, m := range matches {
		if distinct[m] {
			continue
		}
		distinct[m] = true
		info, err := os.Stat(filepath.Join(dir, m))
		if err != nil {
			continue
		}
		total += info.Size()
	}
	if total <= maxReferencedFileTotalBytes {
		return nil
	}
	return &Issue{
		File: skillPath,
		Line: 1,
		Rule: "bloat-referenced-file-size",
		Msg:  fmt.Sprintf("referenced files total %d bytes, over the %d-byte budget", total, maxReferencedFileTotalBytes),
	}
}
