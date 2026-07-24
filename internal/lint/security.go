// Package lint's security.go implements the OWASP Agentic Skills Top 10
// static checks: AST01 (malicious payloads), AST03 (over-privileged
// access), AST04 (insecure metadata parsing), AST10 (cross-platform
// format issues). This is a first-layer static scan, not a malware
// scanner — obfuscated or natural-language-only attacks (OWASP AST08) can
// bypass pattern matching by design.
package lint

import (
	"fmt"
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
