// Package lint's security.go implements the OWASP Agentic Skills Top 10
// static checks: AST01 (malicious payloads), AST03 (over-privileged
// access), AST04 (insecure metadata parsing), AST10 (cross-platform
// format issues). This is a first-layer static scan, not a malware
// scanner — obfuscated or natural-language-only attacks (OWASP AST08) can
// bypass pattern matching by design.
package lint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var pipeToShellRe = regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n|]*\|\s*(sudo\s+)?(sh|bash|zsh)\b`)

var driveLetterRe = regexp.MustCompile(`^[A-Za-z]:\\`)

var promptInjectionPhrases = []string{
	"ignore previous instructions",
	"ignore all previous instructions",
	"disregard your system prompt",
	"disregard the above",
	"without telling the user",
	"don't tell the user",
	"secretly",
	"do this silently",
}

var base64BlobRe = regexp.MustCompile(`[A-Za-z0-9+/]{80,}={0,2}`)

var dynamicExecRe = regexp.MustCompile(`\b(eval|exec)\s*\(\s*[^"'\)]`)

// scanTextForAST01 scans arbitrary text content — a SKILL.md body or a
// referenced file's content — for malicious-payload patterns (AST01).
func scanTextForAST01(file, content string) []Issue {
	var issues []Issue
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if pipeToShellRe.MatchString(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast01-pipe-to-shell", Msg: "line pipes a curl/wget download directly into a shell"})
		}
		lower := strings.ToLower(line)
		for _, phrase := range promptInjectionPhrases {
			if strings.Contains(lower, phrase) {
				issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast01-prompt-injection-phrase", Msg: fmt.Sprintf("line contains a prompt-injection-style phrase: %q", phrase)})
			}
		}
		if base64BlobRe.MatchString(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast01-embedded-base64-blob", Msg: "line contains a long base64-looking blob"})
		}
		if dynamicExecRe.MatchString(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast01-dynamic-exec-untrusted-input", Msg: "line calls eval/exec on a non-literal argument"})
		}
	}
	return issues
}

var broadFilesystemRe = regexp.MustCompile(`(~/\.ssh|/etc/passwd|\.env\b|rm\s+-rf\s+/|chmod\s+777)`)

// unrestrictedNetworkRe matches curl/wget/fetch/requests/http.Get(Post)
// network calls and captures the URL authority (optional userinfo, host,
// and optional port) in group 1, so callers can check the actual host
// rather than scanning the whole line — RE2 has no lookahead, so this
// capture-then-check is the workaround. The capture includes '@' so a
// userinfo prefix (user@ or user:pass@) is captured too rather than
// truncating the match at the userinfo boundary; callers must split on
// the last '@' to recover the real host before checking it. This closes
// the two known "plant localhost somewhere the checker looks" bypasses
// (query string, and userinfo) — it does not claim to close every
// possible way to spoof or obscure a host.
var unrestrictedNetworkRe = regexp.MustCompile(`(?i)(?:(?:curl|wget)\s+https?://|fetch\(\s*['"]https?://|requests\.(?:get|post)\(\s*['"]https?://|http\.(?:Get|Post)\(\s*['"]https?://)([a-zA-Z0-9.\-\[\]:@]+)`)

// scanTextForAST03 scans arbitrary text content for over-privileged-access
// patterns (AST03): broad filesystem access and unrestricted network calls.
func scanTextForAST03(file, content string) []Issue {
	var issues []Issue
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if broadFilesystemRe.MatchString(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast03-broad-filesystem-access", Msg: "line references a sensitive filesystem path or destructive command"})
		}
		if m := unrestrictedNetworkRe.FindStringSubmatch(line); m != nil {
			// Exclude localhost and 127.0.0.1 calls — checked against the
			// captured host only, not the whole line (a substring check
			// against the whole line is bypassable via path/query/comment).
			// The capture may include a userinfo prefix (user@ or
			// user:pass@); the real host is whatever follows the last '@',
			// since that's what a URL parser would treat as the host.
			host := strings.ToLower(m[1])
			if atIdx := strings.LastIndex(host, "@"); atIdx != -1 {
				host = host[atIdx+1:]
			}
			if idx := strings.IndexAny(host, ":"); idx != -1 {
				host = host[:idx]
			}
			if host != "localhost" && host != "127.0.0.1" {
				issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast03-unrestricted-network-call", Msg: "line makes a network call to a non-localhost host"})
			}
		}
	}
	return issues
}

// pathTraversalIssue reports an ast03-path-traversal issue if refPath
// (relative to dir, as extracted from SKILL.md) resolves outside dir.
// Returns nil if refPath stays within dir.
func pathTraversalIssue(skillPath, dir, refPath string, line int) *Issue {
	joined := filepath.Join(dir, refPath)
	rel, err := filepath.Rel(dir, filepath.Clean(joined))
	if err != nil {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &Issue{File: skillPath, Line: line, Rule: "ast03-path-traversal", Msg: fmt.Sprintf("referenced path %q escapes the skill directory", refPath)}
	}
	return nil
}

// ast10PathIssues checks refPath (as extracted from SKILL.md) for
// cross-platform portability problems: backslash separators and absolute
// paths.
func ast10PathIssues(skillPath, refPath string, line int) []Issue {
	var issues []Issue
	if strings.Contains(refPath, `\`) {
		issues = append(issues, Issue{File: skillPath, Line: line, Rule: "ast10-backslash-path-separator", Msg: fmt.Sprintf("referenced path %q uses a backslash separator, breaks on POSIX runners", refPath)})
	}
	if strings.HasPrefix(refPath, "/") || driveLetterRe.MatchString(refPath) {
		issues = append(issues, Issue{File: skillPath, Line: line, Rule: "ast10-absolute-path-reference", Msg: fmt.Sprintf("referenced path %q is absolute, breaks portability across machines", refPath)})
	}
	return issues
}

// caseMismatchIssue reports an ast10-case-mismatch issue if refPath
// (relative to dir) doesn't exist under its exact case, but a
// case-insensitive match does. Returns nil if the exact path exists, or
// if no case-insensitive match exists either (missing-referenced-file
// already covers a fully-missing file).
func caseMismatchIssue(skillPath, dir, refPath string, line int) *Issue {
	// Check if path exists with exact case by walking the directory tree.
	// We can't use os.Stat alone because on case-insensitive filesystems
	// it succeeds even when the case doesn't match. So we walk manually
	// to check for exact-case matches, then case-insensitive matches.
	parts := strings.Split(filepath.ToSlash(refPath), "/")
	current := dir
	for _, part := range parts {
		entries, err := os.ReadDir(current)
		if err != nil {
			return nil
		}
		exact := false
		foundName := ""
		for _, e := range entries {
			if e.Name() == part {
				exact = true
				break
			}
			if strings.EqualFold(e.Name(), part) {
				foundName = e.Name()
			}
		}
		if exact {
			current = filepath.Join(current, part)
			continue
		}
		if foundName != "" {
			return &Issue{File: skillPath, Line: line, Rule: "ast10-case-mismatch", Msg: fmt.Sprintf("referenced path %q differs in case from the file on disk (%q)", refPath, foundName)}
		}
		return nil
	}
	return nil
}

var binaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".pdf": true,
	".zip": true, ".exe": true, ".bin": true,
}

const maxScanBytes = 1 << 20 // 1MB

func looksBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	return bytes.IndexByte(data[:n], 0) != -1
}

// scanReferencedFileContent reads refPath (relative to dir) and runs the
// AST01/AST03 content scanners against it, if the file exists, isn't
// binary, and is under the 1MB scan cap. Returns nil (no issues, no
// error) for any file that doesn't qualify — a missing file is already
// reported by missing-referenced-file, and skillci intentionally doesn't
// scan binary or oversized files.
func scanReferencedFileContent(dir, refPath string) []Issue {
	if binaryExts[strings.ToLower(filepath.Ext(refPath))] {
		return nil
	}
	full := filepath.Join(dir, refPath)
	info, err := os.Stat(full)
	if err != nil || info.Size() > maxScanBytes {
		return nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil
	}
	if looksBinary(data) {
		return nil
	}
	content := string(data)
	var issues []Issue
	issues = append(issues, scanTextForAST01(full, content)...)
	issues = append(issues, scanTextForAST03(full, content)...)
	return issues
}
