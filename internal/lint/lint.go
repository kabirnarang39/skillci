package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kabirnarang39/skillci/internal/evalspec"
)

type Issue struct {
	File string
	Line int
	Rule string
	Msg  string
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// referencedFileRe matches references|scripts|assets paths in the body,
// including Windows-style backslash separators (both as the first
// separator and within the rest of the path) — otherwise a backslash-style
// reference is never extracted at all, and ast10-backslash-path-separator
// (which inspects the extracted match) can never fire.
//
// The keyword is word-boundary-anchored, so this alone can never capture
// an absolute-path prefix that comes before the keyword (e.g. the
// "/home/user/" in "/home/user/scripts/helper.py") — extendWithAbsolutePrefix
// below prepends that prefix onto the match when present, so
// ast10-absolute-path-reference (which inspects the extracted match) can
// fire on real absolute references instead of just their tail.
var referencedFileRe = regexp.MustCompile(`\b(references|scripts|assets)[/\\][A-Za-z0-9_\-./\\]+`)

// isAbsPathPrefixByte reports whether b can appear in the path portion of
// an absolute-path prefix (unix or windows), for the backward scan in
// extendWithAbsolutePrefix.
func isAbsPathPrefixByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '/' || b == '\\' || b == ':' || b == '.' || b == '-' || b == '_':
		return true
	}
	return false
}

// extendWithAbsolutePrefix scans body backward from matchStart (the start
// of a referencedFileRe match) over contiguous path characters, and
// returns that span if it looks like an absolute-path prefix (starts with
// "/" or a drive letter like "C:\"). Returns "" otherwise, including when
// the scan hits a directory separator that isn't itself absolute (e.g.
// "utils/scripts/setup.sh" — "utils/" isn't an absolute prefix, so it's
// left for the existing relative-path handling untouched) — this stops
// the scan from swallowing an unrelated relative prefix, or text from
// earlier in the line, into the match.
func extendWithAbsolutePrefix(body string, matchStart int) string {
	i := matchStart
	for i > 0 && isAbsPathPrefixByte(body[i-1]) {
		i--
	}
	prefix := body[i:matchStart]
	if strings.HasPrefix(prefix, "/") || driveLetterRe.MatchString(prefix) {
		return prefix
	}
	return ""
}

// LintSkill checks a skill folder for the MVP rule set: valid frontmatter,
// required name/description, description length budget, referenced files
// exist, and no obviously-committed secrets in the body.
func LintSkill(dir string) ([]Issue, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", skillPath, err)
	}
	content := string(data)

	var issues []Issue

	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return []Issue{{File: skillPath, Line: 1, Rule: "invalid-frontmatter", Msg: err.Error()}}, nil
	}

	var meta frontmatter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		// A duplicate top-level key is the one typed-unmarshal failure mode
		// AST04 already has a dedicated rule for. Route it through
		// scanFrontmatterSecurity (whose generic *yaml.Node parse doesn't
		// error on duplicate keys) instead of the generic invalid-frontmatter
		// message, so ast04-duplicate-frontmatter-key is actually reachable.
		if strings.Contains(err.Error(), "already defined") {
			// Only the duplicate-key issue is reported here; the rest of
			// LintSkill's checks are skipped, matching the existing
			// invalid-frontmatter early-return precedent above — the
			// duplicate-key issue always fires, so this is never a
			// silently-clean result, just a partial one until the user
			// fixes the frontmatter and re-runs.
			return scanFrontmatterSecurity(skillPath, fm), nil
		}
		return []Issue{{File: skillPath, Line: 1, Rule: "invalid-frontmatter", Msg: err.Error()}}, nil
	}

	if strings.TrimSpace(meta.Name) == "" {
		issues = append(issues, Issue{File: skillPath, Line: 2, Rule: "missing-name", Msg: "frontmatter must set name"})
	}
	if strings.TrimSpace(meta.Description) == "" {
		issues = append(issues, Issue{File: skillPath, Line: 2, Rule: "missing-description", Msg: "frontmatter must set description"})
	} else if len(meta.Description) > 1024 {
		issues = append(issues, Issue{File: skillPath, Line: 2, Rule: "description-too-long", Msg: fmt.Sprintf("description is %d chars, over the 1024 trigger-matching budget", len(meta.Description))})
	}

	issues = append(issues, scanFrontmatterSecurity(skillPath, fm)...)

	for _, loc := range referencedFileRe.FindAllStringIndex(body, -1) {
		rawMatch := body[loc[0]:loc[1]]
		if prefix := extendWithAbsolutePrefix(body, loc[0]); prefix != "" {
			rawMatch = prefix + rawMatch
		}
		match := strings.TrimRight(rawMatch, `\`)
		refPath := filepath.Join(dir, match)
		matchIdx := strings.Index(body, match)
		lineNum := strings.Count(body[:matchIdx], "\n") + 1
		if _, err := os.Stat(refPath); os.IsNotExist(err) {
			issues = append(issues, Issue{File: skillPath, Line: lineNum, Rule: "missing-referenced-file", Msg: fmt.Sprintf("referenced file %s does not exist", match)})
		}
		traversalIssue := pathTraversalIssue(skillPath, dir, match, lineNum)
		if traversalIssue != nil {
			issues = append(issues, *traversalIssue)
		}
		issues = append(issues, ast10PathIssues(skillPath, match, lineNum)...)
		if iss := caseMismatchIssue(skillPath, dir, match, lineNum); iss != nil {
			issues = append(issues, *iss)
		}
		if traversalIssue == nil {
			issues = append(issues, scanReferencedFileContent(dir, match)...)
		}
	}

	issues = append(issues, scanForSecrets(skillPath, body)...)
	issues = append(issues, scanTextForAST01(skillPath, body)...)
	issues = append(issues, scanTextForAST03(skillPath, body)...)

	return issues, nil
}

// LintEvals checks a skill's eval cases (evals/*.yaml) for issues that
// don't require a model call. Kept separate from LintSkill, which only
// reads SKILL.md — eval case data lives in a different file layout and
// loads through evalspec.LoadDir, not anything LintSkill touches.
func LintEvals(dir string) ([]Issue, error) {
	cases, err := evalspec.LoadDir(filepath.Join(dir, "evals"))
	if err != nil {
		return nil, err
	}

	var issues []Issue
	for _, c := range cases {
		if c.Assert.Fuzz != nil && *c.Assert.Fuzz && c.Assert.Triggered == nil {
			issues = append(issues, Issue{
				File: c.SourceFile,
				Line: 1,
				Rule: "fuzz-without-triggered",
				Msg:  fmt.Sprintf("case %q sets fuzz: true without a triggered assertion — fuzzing has nothing to compare mutated outcomes against", c.Name),
			})
		}
		if c.Assert.FuzzStrict != nil && *c.Assert.FuzzStrict && (c.Assert.Fuzz == nil || !*c.Assert.Fuzz) {
			issues = append(issues, Issue{
				File: c.SourceFile,
				Line: 1,
				Rule: "fuzz-strict-without-fuzz",
				Msg:  fmt.Sprintf("case %q sets fuzz_strict: true without fuzz: true — fuzz_strict has no effect unless fuzz is also enabled", c.Name),
			})
		}
	}
	return issues, nil
}

func splitFrontmatter(content string) (fm, body string, err error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", fmt.Errorf("SKILL.md must start with --- frontmatter delimiter")
	}
	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		return "", "", fmt.Errorf("SKILL.md frontmatter is not closed with ---")
	}
	return rest[:idx], rest[idx+5:], nil
}

var secretRe = regexp.MustCompile(`(?i)(sk-ant-[a-z0-9\-_]{10,}|api[_-]?key\s*[:=]\s*['"][a-z0-9]{16,}['"])`)

func scanForSecrets(file, body string) []Issue {
	var issues []Issue
	for i, line := range strings.Split(body, "\n") {
		if secretRe.MatchString(line) {
			issues = append(issues, Issue{File: file, Line: i + 1, Rule: "possible-secret", Msg: "line looks like a committed API key/secret"})
		}
	}
	return issues
}
