// Package lint's security.go implements the OWASP Agentic Skills Top 10
// static checks: AST01 (malicious payloads), AST03 (over-privileged
// access), AST04 (insecure metadata parsing), AST10 (cross-platform
// format issues). This is a first-layer static scan, not a malware
// scanner — obfuscated or natural-language-only attacks (OWASP AST08) can
// bypass pattern matching by design.
package lint

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var pipeToShellRe = regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n|]*\|\s*(sudo\s+)?(sh|bash|zsh)\b`)

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

var unrestrictedNetworkRe = regexp.MustCompile(`(?i)(curl|wget)\s+https?://|fetch\(\s*['"]https?://|requests\.(get|post)\(\s*['"]https?://|http\.(Get|Post)\(`)

// scanTextForAST03 scans arbitrary text content for over-privileged-access
// patterns (AST03): broad filesystem access and unrestricted network calls.
func scanTextForAST03(file, content string) []Issue {
	var issues []Issue
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if broadFilesystemRe.MatchString(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast03-broad-filesystem-access", Msg: "line references a sensitive filesystem path or destructive command"})
		}
		if unrestrictedNetworkRe.MatchString(line) {
			// Exclude localhost and 127.0.0.1 calls
			lowerLine := strings.ToLower(line)
			if !strings.Contains(lowerLine, "localhost") && !strings.Contains(line, "127.0.0.1") {
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
