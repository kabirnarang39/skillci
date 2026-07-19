package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
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

var referencedFileRe = regexp.MustCompile(`\b(references|scripts|assets)/[A-Za-z0-9_\-./]+`)

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

	for _, match := range referencedFileRe.FindAllString(body, -1) {
		refPath := filepath.Join(dir, match)
		if _, err := os.Stat(refPath); os.IsNotExist(err) {
			// Compute line number where the reference appears in body.
			matchIdx := strings.Index(body, match)
			lineNum := strings.Count(body[:matchIdx], "\n") + 1
			issues = append(issues, Issue{File: skillPath, Line: lineNum, Rule: "missing-referenced-file", Msg: fmt.Sprintf("referenced file %s does not exist", match)})
		}
	}

	issues = append(issues, scanForSecrets(skillPath, body)...)

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
