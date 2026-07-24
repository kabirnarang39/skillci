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

	"gopkg.in/yaml.v3"
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

// execConcatCallRe matches the start of an eval(/exec( call whose first
// non-whitespace argument character is a quote — the case dynamicExecRe
// deliberately excludes as "starts with a string literal". Captures the
// quote character so hasExecConcatBypass can find that literal's closing
// quote and inspect only what comes after it.
var execConcatCallRe = regexp.MustCompile(`\b(?:eval|exec)\s*\(\s*(['"])`)

// hasExecConcatBypass reports whether line calls eval/exec with an argument
// that starts as a string literal but then concatenates something onto it
// with '+' before the call's closing paren — e.g. eval("prefix" + user_input)
// — a bypass of dynamicExecRe's "starts with a quote, therefore safe"
// assumption. A '+' character has to be found strictly after the literal's
// own closing quote to count; a '+' that merely appears inside the quoted
// content (e.g. eval("1 + 1")) must not trigger this. A plain regex can't
// make that distinction (Go's RE2 has no way to say "not inside the string
// that started at the previous quote"), hence the closing-quote-first,
// then-check-after approach below instead of a single regex.
func hasExecConcatBypass(line string) bool {
	loc := execConcatCallRe.FindStringSubmatchIndex(line)
	if loc == nil {
		return false
	}
	quote := line[loc[2]]
	afterOpenQuote := line[loc[3]:]
	closeIdx := strings.IndexByte(afterOpenQuote, quote)
	if closeIdx == -1 {
		return false
	}
	afterLiteral := afterOpenQuote[closeIdx+1:]
	if parenIdx := strings.IndexByte(afterLiteral, ')'); parenIdx != -1 {
		afterLiteral = afterLiteral[:parenIdx]
	}
	return strings.IndexByte(afterLiteral, '+') != -1
}

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
		if dynamicExecRe.MatchString(line) || hasExecConcatBypass(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "ast01-dynamic-exec-untrusted-input", Msg: "line calls eval/exec on a non-literal argument or concatenates onto a string literal argument"})
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
			// Bracketed IPv6 loopback ([::1], optionally with a :port after
			// the closing bracket) has its own colons, so it must be
			// recognized before the generic first-colon port-strip below
			// would otherwise mangle it into "[".
			if host == "[::1]" || strings.HasPrefix(host, "[::1]:") {
				host = "::1"
			} else if idx := strings.IndexAny(host, ":"); idx != -1 {
				host = host[:idx]
			}
			if host != "localhost" && host != "127.0.0.1" && host != "::1" {
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

const maxFrontmatterBytes = 4096
const maxFrontmatterDepth = 3

// scanFrontmatterSecurity parses fm as a generic yaml.Node tree (not the
// typed frontmatter struct LintSkill uses, which silently drops unknown
// fields and duplicate keys) and checks for AST04 structural issues. A
// frontmatter that fails to parse at all returns no issues here — that
// failure is already reported by LintSkill's own invalid-frontmatter path.
//
// Empirically, yaml.Unmarshal into a generic *yaml.Node does NOT error on
// duplicate top-level keys — it preserves both key/value pairs in the
// mapping node's Content slice (unlike unmarshaling into a typed struct or
// map, which does reject duplicates). So findDuplicateKey can walk the
// tree directly; no error-path workaround is needed here.
func scanFrontmatterSecurity(skillPath, fm string) []Issue {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &node); err != nil {
		return nil
	}

	var issues []Issue

	oversizedReasons := []string{}
	if len(fm) > maxFrontmatterBytes {
		oversizedReasons = append(oversizedReasons, fmt.Sprintf("%d bytes exceeds the %d-byte budget", len(fm), maxFrontmatterBytes))
	}
	if depth := nodeDepth(&node); depth > maxFrontmatterDepth {
		oversizedReasons = append(oversizedReasons, fmt.Sprintf("nesting depth %d exceeds the %d-level budget", depth, maxFrontmatterDepth))
	}
	if len(oversizedReasons) > 0 {
		issues = append(issues, Issue{File: skillPath, Line: 1, Rule: "ast04-oversized-frontmatter", Msg: "frontmatter is oversized: " + strings.Join(oversizedReasons, "; ")})
	}

	if hasAnchorOrAlias(&node) {
		issues = append(issues, Issue{File: skillPath, Line: 1, Rule: "ast04-yaml-anchor-alias", Msg: "frontmatter uses a YAML anchor/alias, a known parser-expansion-bomb technique"})
	}

	if dupKey := findDuplicateKey(&node); dupKey != "" {
		issues = append(issues, Issue{File: skillPath, Line: 1, Rule: "ast04-duplicate-frontmatter-key", Msg: fmt.Sprintf("frontmatter has a duplicate key %q", dupKey)})
	}

	for _, field := range topLevelKeys(&node) {
		if field != "name" && field != "description" {
			issues = append(issues, Issue{File: skillPath, Line: 1, Rule: "ast04-unexpected-frontmatter-field", Msg: fmt.Sprintf("frontmatter has an unexpected field %q (only name/description are part of the documented spec)", field)})
		}
	}

	return issues
}

// mappingRoot unwraps the DocumentNode yaml.Unmarshal produces when
// parsing into a generic *yaml.Node, returning the actual root mapping.
func mappingRoot(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

func nodeDepth(n *yaml.Node) int {
	root := mappingRoot(n)
	if len(root.Content) == 0 {
		return 0
	}
	max := 0
	for _, c := range root.Content {
		if d := nodeDepth(c); d > max {
			max = d
		}
	}
	return max + 1
}

func hasAnchorOrAlias(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return true
	}
	for _, c := range n.Content {
		if hasAnchorOrAlias(c) {
			return true
		}
	}
	return false
}

func findDuplicateKey(n *yaml.Node) string {
	root := mappingRoot(n)
	if root.Kind != yaml.MappingNode {
		return ""
	}
	seen := map[string]bool{}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if seen[key] {
			return key
		}
		seen[key] = true
	}
	return ""
}

func topLevelKeys(n *yaml.Node) []string {
	root := mappingRoot(n)
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var keys []string
	for i := 0; i < len(root.Content); i += 2 {
		keys = append(keys, root.Content[i].Value)
	}
	return keys
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
	// refPath already passed pathTraversalIssue's textual containment
	// check, but that only reasons about the path string — a symlink at
	// full can still resolve to a target outside dir. Resolve it and
	// re-check containment before reading content, so a symlink can't be
	// used to smuggle an out-of-tree file through the content scanner.
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil
	}
	// Resolve dir too before comparing — on macOS a t.TempDir() (and
	// /tmp generally) lives under a symlink (e.g. /var -> /private/var),
	// so comparing an unresolved dir against a resolved full would flag
	// every in-tree file as "escaping".
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolvedDir = dir
	}
	if rel, err := filepath.Rel(resolvedDir, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	// full (not resolved) is kept for Stat/ReadFile/issue attribution below
	// — os.Stat/os.ReadFile already follow the symlink transparently to
	// read the same (now containment-checked) content, and issues should
	// still be attributed to the path as written in SKILL.md.
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
