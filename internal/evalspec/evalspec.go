package evalspec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Assertions struct {
	Triggered       *bool    `yaml:"triggered"`
	Contains        []string `yaml:"contains"`
	NotContains     []string `yaml:"not_contains"`
	MaxTokensLoaded *int     `yaml:"max_tokens_loaded"`
}

type Case struct {
	Name           string     `yaml:"name"`
	Prompt         string     `yaml:"prompt"`
	SkillUnderTest string     `yaml:"skill_under_test"`
	Assert         Assertions `yaml:"assert"`
	SourceFile     string     `yaml:"-"`
}

// LoadDir reads every *.yaml file directly under evalsDir (not recursively,
// so evals/_generated/*.yaml is excluded by construction — those are
// pending regression-derived cases awaiting `skillci accept`).
func LoadDir(evalsDir string) ([]Case, error) {
	entries, err := os.ReadDir(evalsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cases []Case
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(evalsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var c Case
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		c.SourceFile = path
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}
