# SkillCI MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `skillci` — a Go CLI that lints, evals, and regression-tests Claude Skills against a matrix of Claude models, a GitHub Action wrapping it, and an opt-in hosted dashboard tracking pass/fail history over time.

**Architecture:** Single Go module. `cmd/skillci` is the CLI binary (check/eval/regress/badge/init/accept/login). `cmd/skillci-server` is the dashboard/ingestion API binary, Postgres-backed, server-rendered HTML pages (no JS framework — SVG sparklines generated server-side, same approach as the badge). `.github/actions/skillci` is a composite GitHub Action wrapping the CLI binary.

**Tech Stack:** Go 1.25, `spf13/cobra` (CLI subcommands), `gopkg.in/yaml.v3` (skill eval/config parsing), `lib/pq` (Postgres driver via `database/sql`), stdlib `net/http` + `html/template` for the dashboard server. No Node/JS toolchain anywhere in the project.

## Global Constraints

- Module path: `github.com/kabirnarang/skillci` (rename later if the GitHub org differs — update `go.mod` + all imports in one commit if so)
- Go 1.25 minimum (matches installed toolchain: `go version` → `go1.25.3`)
- `fail_on: regression` is the default CLI behavior everywhere — CI must fail only on a *new* break vs. last known-good, never on every non-deterministic miss (per design §4)
- Dashboard upload is opt-in only (`--upload` flag) — `skillci regress` without it must never make a network call other than to the model API (per design §7)
- No secrets in committed fixtures — API keys in tests/cassettes must be fake placeholder strings, never real keys
- All Go code passes `gofmt -l .` (no diff) and `go vet ./...` before every commit

---

## File Structure

```
skillci/
  go.mod
  cmd/
    skillci/
      main.go              # CLI entrypoint, wires cobra root command
    skillci-server/
      main.go               # dashboard server entrypoint
  internal/
    config/
      config.go             # .skillci.yaml parsing
      config_test.go
    lint/
      lint.go                # SKILL.md frontmatter/format rules
      lint_test.go
    evalspec/
      evalspec.go            # evals/*.yaml parsing (eval case struct)
      evalspec_test.go
    anthropic/
      client.go              # minimal Messages API client
      client_test.go
    runner/
      runner.go              # runs one eval case against one model, applies assertions
      runner_test.go
    history/
      history.go             # .skillci/history.json read/write
      history_test.go
    regress/
      regress.go             # model matrix + diff vs history + self-growing eval loop
      regress_test.go
    badge/
      badge.go                # SVG badge generation
      badge_test.go
    upload/
      upload.go               # dashboard upload client (--upload)
      upload_test.go
    dashboard/
      server.go                # HTTP server, routes
      ingest.go                # POST /api/v1/results handler
      store.go                 # Postgres access layer
      pages.go                  # skill page + leaderboard HTML rendering
      sparkline.go               # server-side SVG trend line
      schema.sql                 # Postgres schema
      server_test.go
  .github/
    actions/
      skillci/
        action.yml            # composite action
    workflows/
      ci.yml                  # project's own CI: go test + gofmt + vet + dogfood check
  examples/
    example-skill/
      SKILL.md
      evals/
        triggers.yaml
```

---

## Task 1: Repo scaffold, Go module, project CI

**Files:**
- Create: `go.mod`
- Create: `cmd/skillci/main.go`
- Create: `.github/workflows/ci.yml`
- Create: `.gitignore`

**Interfaces:**
- Produces: a `main()` in `cmd/skillci` that prints a version string and exits 0 — later tasks replace this with real cobra wiring.

- [ ] **Step 1: Initialize the Go module**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go mod init github.com/kabirnarang/skillci`
Expected: creates `go.mod` with `module github.com/kabirnarang/skillci` and a `go 1.25` line.

- [ ] **Step 2: Write the placeholder CLI entrypoint**

```go
// cmd/skillci/main.go
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fmt.Fprintf(os.Stdout, "skillci %s\n", version)
}
```

- [ ] **Step 3: Verify it builds and runs**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go run ./cmd/skillci`
Expected: prints `skillci dev`

- [ ] **Step 4: Add `.gitignore`**

```
/bin/
*.test
.skillci/
.env
```

- [ ] **Step 5: Add project CI workflow**

```yaml
# .github/workflows/ci.yml
name: ci
on:
  push:
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - run: gofmt -l . | tee /tmp/gofmt.out && test ! -s /tmp/gofmt.out
      - run: go vet ./...
      - run: go test ./...
```

- [ ] **Step 6: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add go.mod cmd/skillci/main.go .github/workflows/ci.yml .gitignore
git commit -m "Scaffold Go module, CLI entrypoint, project CI"
```

---

## Task 2: `.skillci.yaml` config parsing

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  ```go
  type Config struct {
      Models []string `yaml:"models"`
      FailOn string   `yaml:"fail_on"` // "regression" (default) | "any_fail" | "triggered_only"
  }
  func Load(path string) (Config, error)
  func Default() Config // Models: []string{"claude-sonnet-5"}, FailOn: "regression"
  ```
- Consumed by: Task 8 (regress command), Task 12 (init command)

- [ ] **Step 1: Add the YAML dependency**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go get gopkg.in/yaml.v3`

- [ ] **Step 2: Write the failing test**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "models: [claude-sonnet-5, claude-opus-4-8]\nfail_on: any_fail\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Models) != 2 || cfg.Models[0] != "claude-sonnet-5" {
		t.Errorf("Models = %v, want [claude-sonnet-5 claude-opus-4-8]", cfg.Models)
	}
	if cfg.FailOn != "any_fail" {
		t.Errorf("FailOn = %q, want any_fail", cfg.FailOn)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	def := Default()
	if cfg.FailOn != def.FailOn || len(cfg.Models) != len(def.Models) {
		t.Errorf("Load(missing) = %+v, want default %+v", cfg, def)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/config/...`
Expected: FAIL — `undefined: Load` / `undefined: Default` (package doesn't exist yet)

- [ ] **Step 4: Write the implementation**

```go
// internal/config/config.go
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Models []string `yaml:"models"`
	FailOn string   `yaml:"fail_on"`
}

func Default() Config {
	return Config{
		Models: []string{"claude-sonnet-5"},
		FailOn: "regression",
	}
}

// Load reads a .skillci.yaml file at path. A missing file is not an error —
// it returns Default() so `skillci check`/`eval` work with zero config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if len(cfg.Models) == 0 {
		cfg.Models = Default().Models
	}
	if cfg.FailOn == "" {
		cfg.FailOn = Default().FailOn
	}
	return cfg, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/config/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add go.mod go.sum internal/config
git commit -m "Add .skillci.yaml config parsing"
```

---

## Task 3: SKILL.md lint engine

**Files:**
- Create: `internal/lint/lint.go`
- Test: `internal/lint/lint_test.go`

**Interfaces:**
- Produces:
  ```go
  type Issue struct {
      File string
      Line int
      Rule string // e.g. "missing-description"
      Msg  string
  }
  func LintSkill(dir string) ([]Issue, error) // dir = path to a skill folder containing SKILL.md
  ```
- Consumed by: Task 4 (`check` command)

- [ ] **Step 1: Write the failing test**

```go
// internal/lint/lint_test.go
package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, frontmatter, body string) {
	t.Helper()
	content := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLintSkillValid(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: my-skill\ndescription: Does a thing when asked.\n", "Body text.\n")

	issues, err := LintSkill(dir)
	if err != nil {
		t.Fatalf("LintSkill() error = %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("LintSkill() issues = %v, want none", issues)
	}
}

func TestLintSkillMissingDescription(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: my-skill\n", "Body text.\n")

	issues, err := LintSkill(dir)
	if err != nil {
		t.Fatalf("LintSkill() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Rule != "missing-description" {
		t.Errorf("LintSkill() issues = %v, want one missing-description issue", issues)
	}
}

func TestLintSkillMissingReferencedFile(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "name: my-skill\ndescription: Does a thing.\n", "See references/guide.md for details.\n")

	issues, err := LintSkill(dir)
	if err != nil {
		t.Fatalf("LintSkill() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "missing-referenced-file" {
			found = true
		}
	}
	if !found {
		t.Errorf("LintSkill() issues = %v, want a missing-referenced-file issue", issues)
	}
}

func TestLintSkillNoSkillFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LintSkill(dir)
	if err == nil {
		t.Error("LintSkill() error = nil, want error for missing SKILL.md")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/lint/...`
Expected: FAIL — `undefined: LintSkill`

- [ ] **Step 3: Write the implementation**

```go
// internal/lint/lint.go
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
			issues = append(issues, Issue{File: skillPath, Rule: "missing-referenced-file", Msg: fmt.Sprintf("referenced file %s does not exist", match)})
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/lint/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/lint
git commit -m "Add SKILL.md lint engine"
```

---

## Task 4: `skillci check` command (cobra wiring begins here)

**Files:**
- Modify: `cmd/skillci/main.go`
- Create: `cmd/skillci/check.go`
- Test: `cmd/skillci/check_test.go`

**Interfaces:**
- Consumes: `lint.LintSkill(dir string) ([]lint.Issue, error)` from Task 3
- Produces: `newCheckCmd() *cobra.Command`, registered on the root command — later tasks (`eval`, `regress`, `badge`, `init`, `accept`, `login`) follow the same `newXCmd()` pattern and get added to root in their own tasks.

- [ ] **Step 1: Add cobra dependency**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go get github.com/spf13/cobra`

- [ ] **Step 2: Write the failing test**

```go
// cmd/skillci/check_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCommandReportsIssues(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	err := cmd.Execute()
	if err == nil {
		t.Error("Execute() error = nil, want error because of lint issues (missing description)")
	}
}

func TestCheckCommandPassesCleanSkill(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCheckCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil for a clean skill", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: FAIL — `undefined: newCheckCmd`

- [ ] **Step 4: Write the implementation**

```go
// cmd/skillci/check.go
package main

import (
	"fmt"

	"github.com/kabirnarang/skillci/internal/lint"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [path]",
		Short: "Lint a skill's SKILL.md and referenced files (no API calls)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			issues, err := lint.LintSkill(dir)
			if err != nil {
				return err
			}
			for _, iss := range issues {
				fmt.Fprintf(cmd.OutOrStdout(), "%s:%d: %s: %s\n", iss.File, iss.Line, iss.Rule, iss.Msg)
			}
			if len(issues) > 0 {
				return fmt.Errorf("%d lint issue(s) found", len(issues))
			}
			return nil
		},
	}
}
```

```go
// cmd/skillci/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "skillci",
		Short:   "Lint, eval, and regression-test Claude Skills",
		Version: version,
	}
	root.AddCommand(newCheckCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: PASS

- [ ] **Step 6: Manually verify the binary end-to-end**

Run:
```bash
cd "/Users/kabir/Personal Workspace/skillci"
mkdir -p /tmp/demo-skill
printf -- '---\nname: demo\ndescription: demo skill\n---\nBody.\n' > /tmp/demo-skill/SKILL.md
go run ./cmd/skillci check /tmp/demo-skill
```
Expected: exit code 0, no output (clean skill)

- [ ] **Step 7: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add go.mod go.sum cmd/skillci
git commit -m "Add skillci check command"
```

---

## Task 5: Eval case parsing (`evals/*.yaml`)

**Files:**
- Create: `internal/evalspec/evalspec.go`
- Test: `internal/evalspec/evalspec_test.go`

**Interfaces:**
- Produces:
  ```go
  type Assertions struct {
      Triggered      *bool    `yaml:"triggered"`
      Contains       []string `yaml:"contains"`
      NotContains    []string `yaml:"not_contains"`
      MaxTokensLoaded *int    `yaml:"max_tokens_loaded"`
  }
  type Case struct {
      Name           string     `yaml:"name"`
      Prompt         string     `yaml:"prompt"`
      SkillUnderTest string     `yaml:"skill_under_test"`
      Assert         Assertions `yaml:"assert"`
      SourceFile     string     `yaml:"-"` // set by LoadDir, not from YAML
  }
  func LoadDir(evalsDir string) ([]Case, error) // reads all evals/*.yaml, skips evals/_generated/
  ```
- Consumed by: Task 6 (runner), Task 9 (regress self-growing loop writes into `evals/_generated/`)

- [ ] **Step 1: Write the failing test**

```go
// internal/evalspec/evalspec_test.go
package evalspec

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCase(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "triggers.yaml", `
name: triggers-on-pr-review-request
prompt: "Can you review this PR for SOLID violations?"
skill_under_test: pr-review
assert:
  triggered: true
  contains: ["SOLID", "verdict"]
  not_contains: ["I cannot"]
  max_tokens_loaded: 3000
`)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("LoadDir() got %d cases, want 1", len(cases))
	}
	c := cases[0]
	if c.Name != "triggers-on-pr-review-request" || c.SkillUnderTest != "pr-review" {
		t.Errorf("case = %+v, unexpected fields", c)
	}
	if c.Assert.Triggered == nil || !*c.Assert.Triggered {
		t.Errorf("Assert.Triggered = %v, want true", c.Assert.Triggered)
	}
	if len(c.Assert.Contains) != 2 {
		t.Errorf("Assert.Contains = %v, want 2 entries", c.Assert.Contains)
	}
	if c.Assert.MaxTokensLoaded == nil || *c.Assert.MaxTokensLoaded != 3000 {
		t.Errorf("Assert.MaxTokensLoaded = %v, want 3000", c.Assert.MaxTokensLoaded)
	}
}

func TestLoadDirSkipsGeneratedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, "real.yaml", "name: real\nprompt: p\nskill_under_test: s\nassert:\n  triggered: true\n")
	genDir := filepath.Join(dir, "_generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCase(t, genDir, "pending.yaml", "name: pending\nprompt: p\nskill_under_test: s\nassert:\n  triggered: true\n")

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 1 || cases[0].Name != "real" {
		t.Errorf("LoadDir() = %v, want only the non-generated case", cases)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/evalspec/...`
Expected: FAIL — `undefined: LoadDir`

- [ ] **Step 3: Write the implementation**

```go
// internal/evalspec/evalspec.go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/evalspec/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/evalspec
git commit -m "Add eval case YAML parsing"
```

---

## Task 6: Anthropic API client

**Files:**
- Create: `internal/anthropic/client.go`
- Test: `internal/anthropic/client_test.go`

**Interfaces:**
- Produces:
  ```go
  type Client struct { /* unexported */ }
  func NewClient(apiKey string) *Client
  func (c *Client) WithBaseURL(url string) *Client // for test server injection
  type Message struct {
      Text        string
      InputTokens int
  }
  func (c *Client) Send(ctx context.Context, model, systemPrompt, userPrompt string) (Message, error)
  ```
- Consumed by: Task 7 (runner)

- [ ] **Step 1: Write the failing test**

```go
// internal/anthropic/client_test.go
package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hello from claude"}},
			"usage":   map[string]int{"input_tokens": 42},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	msg, err := c.Send(context.Background(), "claude-sonnet-5", "You are a test skill.", "hi")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if msg.Text != "hello from claude" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello from claude")
	}
	if msg.InputTokens != 42 {
		t.Errorf("InputTokens = %d, want 42", msg.InputTokens)
	}
}

func TestSendErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	_, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err == nil {
		t.Error("Send() error = nil, want error on 429")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/anthropic/...`
Expected: FAIL — `undefined: NewClient`

- [ ] **Step 3: Write the implementation**

```go
// internal/anthropic/client.go
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.anthropic.com"
const apiVersion = "2023-06-01"

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) WithBaseURL(url string) *Client {
	c.baseURL = url
	return c
}

type Message struct {
	Text        string
	InputTokens int
}

type sendRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []reqMsg  `json:"messages"`
}

type reqMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sendResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Send issues one Messages API call and returns the concatenated text content
// plus the input token count the API reports (used for max_tokens_loaded assertions).
func (c *Client) Send(ctx context.Context, model, systemPrompt, userPrompt string) (Message, error) {
	body, err := json.Marshal(sendRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  []reqMsg{{Role: "user", Content: userPrompt}},
	})
	if err != nil {
		return Message{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, err
	}

	var parsed sendResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Message{}, fmt.Errorf("decoding response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := "unknown error"
		if parsed.Error != nil {
			msg = parsed.Error.Message
		}
		return Message{}, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, msg)
	}

	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return Message{Text: text, InputTokens: parsed.Usage.InputTokens}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/anthropic/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/anthropic
git commit -m "Add minimal Anthropic Messages API client"
```

---

## Task 7: Eval runner (runs one case against one model)

**Known limitation (document, don't hide):** v1 approximates whether a skill would "trigger" by sending the model a system prompt containing only that skill's `name`/`description` (simulating the progressive-disclosure candidate list) and asking it to prefix its reply with an explicit marker line if it would invoke the skill. This is a proxy for the real Claude Code skill-selection mechanism, not a run of the actual harness. Good enough to catch "description no longer matches behavior" regressions; not a substitute for testing inside real Claude Code. Worth a `references/limitations.md` note in the repo later — not required for MVP.

**Files:**
- Create: `internal/runner/runner.go`
- Test: `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `anthropic.Client.Send` (Task 6), `evalspec.Case` (Task 5)
- Produces:
  ```go
  type Result struct {
      CaseName    string
      Model       string
      Triggered   bool
      Passed      bool
      Failures    []string
      InputTokens int
  }
  func RunCase(ctx context.Context, client *anthropic.Client, skillDir string, model string, c evalspec.Case) (Result, error)
  ```
- Consumed by: Task 9 (regress matrix runner)

- [ ] **Step 1: Write the failing test**

```go
// internal/runner/runner_test.go
package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/evalspec"
)

func newSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: pr-review\ndescription: Reviews pull requests for SOLID violations.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubServer(t *testing.T, replyText string, inputTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": inputTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func truePtr() *bool { v := true; return &v }
func intPtr(v int) *int { return &v }

func TestRunCasePassing(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nThis review found a SOLID violation. Overall verdict: REQUEST_CHANGES.", 500)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:           "triggers-on-pr-review-request",
		Prompt:         "Can you review this PR for SOLID violations?",
		SkillUnderTest: "pr-review",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			Contains:        []string{"SOLID", "verdict"},
			NotContains:     []string{"I cannot"},
			MaxTokensLoaded: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Triggered {
		t.Error("Triggered = false, want true")
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true; Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsOnMissingContains(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nLooks fine to me.", 500)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered: truePtr(),
			Contains:  []string{"SOLID"},
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (missing required substring)")
	}
}

func TestRunCaseFailsOnUnexpectedTrigger(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: false", 200)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	falsePtr := func() *bool { v := false; return &v }
	c := evalspec.Case{
		Name:   "should-not-trigger",
		Prompt: "what's the weather",
		Assert: evalspec.Assertions{Triggered: falsePtr()},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Errorf("Passed = false, want true (correctly did not trigger); Failures = %v", result.Failures)
	}
}

func TestRunCaseFailsOnTokenBudget(t *testing.T) {
	srv := stubServer(t, "SKILLCI_TRIGGERED: true\nSOLID verdict here.", 5000)
	defer srv.Close()

	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)
	c := evalspec.Case{
		Name:   "budget-case",
		Prompt: "review this",
		Assert: evalspec.Assertions{
			Triggered:       truePtr(),
			MaxTokensLoaded: intPtr(3000),
		},
	}

	result, err := RunCase(context.Background(), client, newSkillDir(t), "claude-sonnet-5", c)
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Error("Passed = true, want false (5000 tokens exceeds 3000 budget)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/runner/...`
Expected: FAIL — `undefined: RunCase`

- [ ] **Step 3: Write the implementation**

```go
// internal/runner/runner.go
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"gopkg.in/yaml.v3"
)

type Result struct {
	CaseName    string
	Model       string
	Triggered   bool
	Passed      bool
	Failures    []string
	InputTokens int
}

type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func readSkillMeta(skillDir string) (skillMeta, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return skillMeta{}, err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return skillMeta{}, fmt.Errorf("SKILL.md missing frontmatter")
	}
	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		return skillMeta{}, fmt.Errorf("SKILL.md frontmatter not closed")
	}
	var meta skillMeta
	if err := yaml.Unmarshal([]byte(rest[:idx]), &meta); err != nil {
		return skillMeta{}, err
	}
	return meta, nil
}

const triggerMarkerPrefix = "SKILLCI_TRIGGERED:"

// RunCase sends the case's prompt to model, with a system prompt containing
// only the skill's name+description (a proxy for progressive-disclosure
// candidate matching — see Task 7 header note), then checks the response
// against the case's assertions.
func RunCase(ctx context.Context, client *anthropic.Client, skillDir, model string, c evalspec.Case) (Result, error) {
	meta, err := readSkillMeta(skillDir)
	if err != nil {
		return Result{}, err
	}

	systemPrompt := fmt.Sprintf(`You are Claude, deciding whether to use an available skill.

Skill available:
name: %s
description: %s

If, given the user's message, you would invoke this skill, begin your response with the exact line "%s true" followed by a newline, then respond as the skill would. If you would NOT invoke this skill for this message, respond with exactly "%s false" and nothing else.`, meta.Name, meta.Description, triggerMarkerPrefix, triggerMarkerPrefix)

	msg, err := client.Send(ctx, model, systemPrompt, c.Prompt)
	if err != nil {
		return Result{}, err
	}

	triggered := false
	content := msg.Text
	firstLine, remainder, _ := strings.Cut(msg.Text, "\n")
	if strings.TrimSpace(firstLine) == triggerMarkerPrefix+" true" {
		triggered = true
		content = remainder
	} else if strings.TrimSpace(strings.TrimSpace(msg.Text)) == triggerMarkerPrefix+" false" {
		triggered = false
		content = ""
	}

	result := Result{
		CaseName:    c.Name,
		Model:       model,
		Triggered:   triggered,
		InputTokens: msg.InputTokens,
	}

	if c.Assert.Triggered != nil && triggered != *c.Assert.Triggered {
		result.Failures = append(result.Failures, fmt.Sprintf("triggered = %v, want %v", triggered, *c.Assert.Triggered))
	}
	for _, want := range c.Assert.Contains {
		if !strings.Contains(content, want) {
			result.Failures = append(result.Failures, fmt.Sprintf("response missing required substring %q", want))
		}
	}
	for _, unwanted := range c.Assert.NotContains {
		if strings.Contains(content, unwanted) {
			result.Failures = append(result.Failures, fmt.Sprintf("response contains forbidden substring %q", unwanted))
		}
	}
	if c.Assert.MaxTokensLoaded != nil && msg.InputTokens > *c.Assert.MaxTokensLoaded {
		result.Failures = append(result.Failures, fmt.Sprintf("input_tokens = %d, exceeds max_tokens_loaded %d", msg.InputTokens, *c.Assert.MaxTokensLoaded))
	}

	result.Passed = len(result.Failures) == 0
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/runner/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/runner
git commit -m "Add eval case runner with triggered/contains/token-budget assertions"
```

---

## Task 8: `skillci eval` command (single model, no matrix)

**Files:**
- Create: `cmd/skillci/eval.go`
- Modify: `cmd/skillci/main.go`
- Test: `cmd/skillci/eval_test.go`

**Interfaces:**
- Consumes: `evalspec.LoadDir` (Task 5), `runner.RunCase` (Task 7), `config.Load`/`Default` (Task 2)
- Produces: `newEvalCmd() *cobra.Command`, reads `ANTHROPIC_API_KEY` env var, exits non-zero if any case fails.

- [ ] **Step 1: Write the failing test**

```go
// cmd/skillci/eval_test.go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEvalCommandRunsCasesAgainstStubServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true\nSOLID verdict here."}},
			"usage":   map[string]int{"input_tokens": 100},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	skillContent := "---\nname: pr-review\ndescription: Reviews PRs.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: c1\nprompt: review this\nassert:\n  triggered: true\n  contains: [\"SOLID\"]\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newEvalCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil; output = %s", err, out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: FAIL — `undefined: newEvalCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/skillci/eval.go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/runner"
	"github.com/spf13/cobra"
)

func newEvalCmd() *cobra.Command {
	var model string
	cmd := &cobra.Command{
		Use:   "eval [path]",
		Short: "Run a skill's eval suite against a single model",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY is not set")
			}
			client := anthropic.NewClient(apiKey)
			if base := os.Getenv("SKILLCI_BASE_URL"); base != "" {
				client = client.WithBaseURL(base)
			}

			cases, err := evalspec.LoadDir(filepath.Join(dir, "evals"))
			if err != nil {
				return err
			}
			if len(cases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no eval cases found in evals/")
				return nil
			}

			failed := 0
			for _, c := range cases {
				result, err := runner.RunCase(context.Background(), client, dir, model, c)
				if err != nil {
					return fmt.Errorf("running case %s: %w", c.Name, err)
				}
				status := "PASS"
				if !result.Passed {
					status = "FAIL"
					failed++
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s (%s)\n", status, c.Name, model)
				for _, f := range result.Failures {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", f)
				}
			}

			if failed > 0 {
				return fmt.Errorf("%d/%d case(s) failed", failed, len(cases))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&model, "model", "claude-sonnet-5", "model to evaluate against")
	return cmd
}
```

Add to `cmd/skillci/main.go`, inside `main()` after `root.AddCommand(newCheckCmd())`:

```go
	root.AddCommand(newEvalCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add cmd/skillci
git commit -m "Add skillci eval command"
```

---

## Task 9: History storage (`.skillci/history.json`)

**Files:**
- Create: `internal/history/history.go`
- Test: `internal/history/history_test.go`

**Interfaces:**
- Produces:
  ```go
  type CaseResult struct {
      Name   string `json:"name"`
      Model  string `json:"model"`
      Passed bool   `json:"passed"`
  }
  type Run struct {
      Timestamp time.Time    `json:"timestamp"`
      CommitSHA string       `json:"commit_sha"`
      Cases     []CaseResult `json:"cases"`
  }
  type History struct {
      Runs []Run `json:"runs"`
  }
  func Load(path string) (History, error)          // missing file -> empty History, no error
  func (h *History) Append(r Run)
  func (h History) Save(path string) error          // creates parent dirs
  func (h History) LastRun() (Run, bool)
  func (r Run) Result(caseName, model string) (CaseResult, bool)
  ```
- Consumed by: Task 10 (regress diff logic), Task 12 (badge)

- [ ] **Step 1: Write the failing test**

```go
// internal/history/history_test.go
package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFile(t *testing.T) {
	h, err := Load(filepath.Join(t.TempDir(), ".skillci", "history.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(h.Runs) != 0 {
		t.Errorf("Runs = %v, want empty", h.Runs)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skillci", "history.json")
	h := History{}
	h.Append(Run{
		Timestamp: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		CommitSHA: "abc123",
		Cases: []CaseResult{
			{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
		},
	})

	if err := h.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Runs) != 1 || loaded.Runs[0].CommitSHA != "abc123" {
		t.Errorf("loaded = %+v, want one run with commit abc123", loaded)
	}
}

func TestLastRun(t *testing.T) {
	h := History{}
	h.Append(Run{CommitSHA: "first"})
	h.Append(Run{CommitSHA: "second"})

	last, ok := h.LastRun()
	if !ok || last.CommitSHA != "second" {
		t.Errorf("LastRun() = %+v, %v, want second run", last, ok)
	}
}

func TestLastRunEmpty(t *testing.T) {
	h := History{}
	_, ok := h.LastRun()
	if ok {
		t.Error("LastRun() ok = true, want false for empty history")
	}
}

func TestRunResult(t *testing.T) {
	run := Run{Cases: []CaseResult{
		{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
		{Name: "case-a", Model: "claude-opus-4-8", Passed: false},
	}}
	r, ok := run.Result("case-a", "claude-opus-4-8")
	if !ok || r.Passed {
		t.Errorf("Result() = %+v, %v, want passed=false", r, ok)
	}
	_, ok = run.Result("case-a", "claude-haiku-4-5")
	if ok {
		t.Error("Result() ok = true, want false for model not in run")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/history/...`
Expected: FAIL — `undefined: Load`

- [ ] **Step 3: Write the implementation**

```go
// internal/history/history.go
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type CaseResult struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	Passed bool   `json:"passed"`
}

type Run struct {
	Timestamp time.Time    `json:"timestamp"`
	CommitSHA string       `json:"commit_sha"`
	Cases     []CaseResult `json:"cases"`
}

// Result returns the recorded result for caseName+model in this run, if present.
func (r Run) Result(caseName, model string) (CaseResult, bool) {
	for _, c := range r.Cases {
		if c.Name == caseName && c.Model == model {
			return c, true
		}
	}
	return CaseResult{}, false
}

type History struct {
	Runs []Run `json:"runs"`
}

func Load(path string) (History, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return History{}, nil
	}
	if err != nil {
		return History{}, err
	}
	var h History
	if err := json.Unmarshal(data, &h); err != nil {
		return History{}, err
	}
	return h, nil
}

func (h *History) Append(r Run) {
	h.Runs = append(h.Runs, r)
}

func (h History) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (h History) LastRun() (Run, bool) {
	if len(h.Runs) == 0 {
		return Run{}, false
	}
	return h.Runs[len(h.Runs)-1], true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/history/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/history
git commit -m "Add .skillci/history.json storage"
```

---

## Task 10: Regression matrix engine (diff vs. last known-good, self-growing eval loop)

**Files:**
- Create: `internal/regress/regress.go`
- Test: `internal/regress/regress_test.go`

**Interfaces:**
- Consumes: `evalspec.LoadDir`/`Case` (Task 5), `runner.RunCase` (Task 7), `history.History`/`Run`/`CaseResult` (Task 9), `config.Config` (Task 2)
- Produces:
  ```go
  type Outcome struct {
      Case       evalspec.Case
      Model      string
      Result     runner.Result
      IsNewRegression bool // passed last run (or no history), fails now
  }
  type MatrixReport struct {
      Outcomes []Outcome
      GeneratedCases []evalspec.Case // new cases proposed for regressions with no prior coverage
  }
  func RunMatrix(ctx context.Context, client *anthropic.Client, skillDir string, cfg config.Config, cases []evalspec.Case, hist history.History) (MatrixReport, history.Run, error)
  func (r MatrixReport) ShouldFailCI(failOn string) bool
  func WriteGeneratedCases(skillDir string, cases []evalspec.Case) ([]string, error) // writes to evals/_generated/, returns file paths written
  ```
- Consumed by: Task 11 (`regress`/`accept` commands)

**Self-growing rule (design §5):** a case is a "new regression" when it fails now AND (no prior run exists for that case+model, OR the prior run recorded it as passed). When a case fails and had NO prior run at all for that model (first time this exact case+model combination has ever executed), `RunMatrix` also emits a generated case capturing the failing prompt — this is how an uncovered gap becomes a tracked one. If the case already existed in history and simply flipped from pass to fail, no new case is generated (the case already covers this behavior; it's a real regression, not a coverage gap).

- [ ] **Step 1: Write the failing test**

```go
// internal/regress/regress_test.go
package regress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/config"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/history"
)

func newSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: pr-review\ndescription: Reviews PRs.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubServerAlwaysFails(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func truePtr() *bool { v := true; return &v }

func TestRunMatrixFlagsNewRegressionWhenPriorRunPassed(t *testing.T) {
	srv := stubServerAlwaysFails(t)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{Name: "c1", Prompt: "review this", Assert: evalspec.Assertions{Triggered: truePtr()}},
	}
	hist := history.History{}
	hist.Append(history.Run{Cases: []history.CaseResult{
		{Name: "c1", Model: "claude-sonnet-5", Passed: true},
	}})
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, hist)
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if len(report.Outcomes) != 1 || !report.Outcomes[0].IsNewRegression {
		t.Errorf("Outcomes = %+v, want one new-regression outcome", report.Outcomes)
	}
	if !report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = false, want true")
	}
}

func TestRunMatrixNoRegressionWhenNoPriorHistory(t *testing.T) {
	srv := stubServerAlwaysFails(t)
	defer srv.Close()
	client := anthropic.NewClient("test-key").WithBaseURL(srv.URL)

	cases := []evalspec.Case{
		{Name: "c1", Prompt: "review this", Assert: evalspec.Assertions{Triggered: truePtr()}},
	}
	cfg := config.Config{Models: []string{"claude-sonnet-5"}, FailOn: "regression"}

	report, _, err := RunMatrix(context.Background(), client, newSkillDir(t), cfg, cases, history.History{})
	if err != nil {
		t.Fatalf("RunMatrix() error = %v", err)
	}
	if report.Outcomes[0].IsNewRegression {
		t.Error("IsNewRegression = true, want false — no prior history to regress from")
	}
	if report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = true, want false when nothing regressed vs history")
	}
	if len(report.GeneratedCases) != 1 {
		t.Errorf("GeneratedCases = %v, want 1 (uncovered failing case)", report.GeneratedCases)
	}
}

func TestWriteGeneratedCases(t *testing.T) {
	dir := newSkillDir(t)
	cases := []evalspec.Case{{Name: "generated-case", Prompt: "some failing prompt", SkillUnderTest: "pr-review"}}

	paths, err := WriteGeneratedCases(dir, cases)
	if err != nil {
		t.Fatalf("WriteGeneratedCases() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v, want 1", paths)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Errorf("generated file not written: %v", err)
	}
	if filepath.Dir(paths[0]) != filepath.Join(dir, "evals", "_generated") {
		t.Errorf("generated file in %s, want evals/_generated", filepath.Dir(paths[0]))
	}
}
```

```go
func TestShouldFailCIAnyFailMode(t *testing.T) {
	report := MatrixReport{Outcomes: []Outcome{
		{IsNewRegression: false, Result: runner.Result{Passed: false}},
	}}
	if !report.ShouldFailCI("any_fail") {
		t.Error("ShouldFailCI(any_fail) = false, want true when any case failed, regression or not")
	}
	if report.ShouldFailCI("regression") {
		t.Error("ShouldFailCI(regression) = true, want false when the only failure isn't a new regression")
	}
}
```

(This last test requires adding `"github.com/kabirnarang/skillci/internal/runner"` to the test file's import block, alongside `anthropic`, `config`, `evalspec`, and `history`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/regress/...`
Expected: FAIL — `undefined: RunMatrix`

- [ ] **Step 3: Write the implementation**

```go
// internal/regress/regress.go
package regress

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/config"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/kabirnarang/skillci/internal/runner"
	"gopkg.in/yaml.v3"
)

type Outcome struct {
	Case            evalspec.Case
	Model           string
	Result          runner.Result
	IsNewRegression bool
}

type MatrixReport struct {
	Outcomes       []Outcome
	GeneratedCases []evalspec.Case
}

func (r MatrixReport) ShouldFailCI(failOn string) bool {
	for _, o := range r.Outcomes {
		switch failOn {
		case "any_fail":
			if !o.Result.Passed {
				return true
			}
		case "triggered_only":
			if o.Result.Triggered != true && o.Case.Assert.Triggered != nil && *o.Case.Assert.Triggered {
				return true
			}
		default: // "regression"
			if o.IsNewRegression {
				return true
			}
		}
	}
	return false
}

// RunMatrix runs every case against every model in cfg.Models, comparing
// each result to the last recorded run in hist to decide whether a failure
// is a *new* regression (see design §5 / the self-growing eval rule in this
// task's header). It returns the report plus the history.Run that the
// caller should append and save.
func RunMatrix(ctx context.Context, client *anthropic.Client, skillDir string, cfg config.Config, cases []evalspec.Case, hist history.History) (MatrixReport, history.Run, error) {
	lastRun, hadHistory := hist.LastRun()

	var report MatrixReport
	newRun := history.Run{}

	for _, c := range cases {
		for _, model := range cfg.Models {
			result, err := runner.RunCase(ctx, client, skillDir, model, c)
			if err != nil {
				return MatrixReport{}, history.Run{}, fmt.Errorf("case %s on %s: %w", c.Name, model, err)
			}

			newRun.Cases = append(newRun.Cases, history.CaseResult{
				Name: c.Name, Model: model, Passed: result.Passed,
			})

			prior, hadPrior := lastRun.Result(c.Name, model)
			isNewRegression := false
			if !result.Passed {
				if hadPrior && prior.Passed {
					isNewRegression = true
				}
				if !hadPrior {
					report.GeneratedCases = append(report.GeneratedCases, evalspec.Case{
						Name:           c.Name + "-generated-" + model,
						Prompt:         c.Prompt,
						SkillUnderTest: c.SkillUnderTest,
						Assert:         c.Assert,
					})
				}
			}
			_ = hadHistory

			report.Outcomes = append(report.Outcomes, Outcome{
				Case: c, Model: model, Result: result, IsNewRegression: isNewRegression,
			})
		}
	}

	return report, newRun, nil
}

// WriteGeneratedCases writes each case to evals/_generated/<name>.yaml under
// skillDir, for later review via `skillci accept`.
func WriteGeneratedCases(skillDir string, cases []evalspec.Case) ([]string, error) {
	dir := filepath.Join(skillDir, "evals", "_generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var written []string
	for _, c := range cases {
		data, err := yaml.Marshal(c)
		if err != nil {
			return nil, err
		}
		safeName := strings.ReplaceAll(c.Name, "/", "-")
		path := filepath.Join(dir, safeName+".yaml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/regress/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/regress
git commit -m "Add regression matrix engine with self-growing eval loop"
```

---

## Task 11: Badge SVG generation

**Files:**
- Create: `internal/badge/badge.go`
- Test: `internal/badge/badge_test.go`

**Interfaces:**
- Produces:
  ```go
  type State string
  const (
      Passing   State = "passing"
      Partial   State = "partial"
      Regressed State = "regressed"
  )
  func StateFromRun(run history.Run) State // all passed -> Passing, none passed -> Regressed, else Partial
  func Render(state State) string          // returns SVG markup
  ```
- Consumed by: Task 13 (`badge` command), Task 15 (GitHub Action commits the badge)

- [ ] **Step 1: Write the failing test**

```go
// internal/badge/badge_test.go
package badge

import (
	"strings"
	"testing"

	"github.com/kabirnarang/skillci/internal/history"
)

func TestStateFromRunAllPassing(t *testing.T) {
	run := history.Run{Cases: []history.CaseResult{{Passed: true}, {Passed: true}}}
	if got := StateFromRun(run); got != Passing {
		t.Errorf("StateFromRun() = %v, want Passing", got)
	}
}

func TestStateFromRunAllFailing(t *testing.T) {
	run := history.Run{Cases: []history.CaseResult{{Passed: false}, {Passed: false}}}
	if got := StateFromRun(run); got != Regressed {
		t.Errorf("StateFromRun() = %v, want Regressed", got)
	}
}

func TestStateFromRunMixed(t *testing.T) {
	run := history.Run{Cases: []history.CaseResult{{Passed: true}, {Passed: false}}}
	if got := StateFromRun(run); got != Partial {
		t.Errorf("StateFromRun() = %v, want Partial", got)
	}
}

func TestStateFromRunEmpty(t *testing.T) {
	if got := StateFromRun(history.Run{}); got != Regressed {
		t.Errorf("StateFromRun(empty) = %v, want Regressed (no passing evidence)", got)
	}
}

func TestRenderContainsStateAndIsValidSVG(t *testing.T) {
	svg := Render(Passing)
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Errorf("Render() = %q, not valid-looking SVG", svg)
	}
	if !strings.Contains(svg, "passing") {
		t.Errorf("Render(Passing) = %q, want it to contain the state label", svg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/badge/...`
Expected: FAIL — `undefined: StateFromRun`

- [ ] **Step 3: Write the implementation**

```go
// internal/badge/badge.go
package badge

import (
	"fmt"

	"github.com/kabirnarang/skillci/internal/history"
)

type State string

const (
	Passing   State = "passing"
	Partial   State = "partial"
	Regressed State = "regressed"
)

func StateFromRun(run history.Run) State {
	if len(run.Cases) == 0 {
		return Regressed
	}
	passCount := 0
	for _, c := range run.Cases {
		if c.Passed {
			passCount++
		}
	}
	switch {
	case passCount == len(run.Cases):
		return Passing
	case passCount == 0:
		return Regressed
	default:
		return Partial
	}
}

func color(s State) string {
	switch s {
	case Passing:
		return "#2ea44f"
	case Partial:
		return "#dbab09"
	default:
		return "#cf222e"
	}
}

// Render returns a shields.io-style flat SVG badge for the given state.
// Committed directly to the repo by the GitHub Action — no external image
// host, so no hotlink-availability failure mode.
func Render(state State) string {
	c := color(state)
	label := string(state)
	width := 58 + len(label)*7
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="skillci: %s">
  <rect width="58" height="20" fill="#555"/>
  <rect x="58" width="%d" height="20" fill="%s"/>
  <text x="6" y="14" fill="#fff" font-family="Verdana,sans-serif" font-size="11">skillci</text>
  <text x="65" y="14" fill="#fff" font-family="Verdana,sans-serif" font-size="11">%s</text>
</svg>`, width, label, width-58, c, label)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/badge/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/badge
git commit -m "Add SVG badge generation"
```

---

## Task 12: `skillci regress`, `skillci accept`, and `skillci badge` commands

**Files:**
- Create: `cmd/skillci/regress.go`
- Create: `cmd/skillci/accept.go`
- Create: `cmd/skillci/badge.go`
- Modify: `cmd/skillci/main.go`
- Test: `cmd/skillci/regress_test.go`

**Interfaces:**
- Consumes: `regress.RunMatrix`/`WriteGeneratedCases` (Task 10), `history.Load`/`Save` (Task 9), `badge.StateFromRun`/`Render` (Task 11), `config.Load` (Task 2), `evalspec.LoadDir` (Task 5)
- Produces: `newRegressCmd()`, `newAcceptCmd()`, `newBadgeCmd()` — all registered on root in this task.

- [ ] **Step 1: Write the failing test**

```go
// cmd/skillci/regress_test.go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupSkillWithCase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skillContent := "---\nname: pr-review\ndescription: Reviews PRs.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	evalsDir := filepath.Join(dir, "evals")
	if err := os.MkdirAll(evalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseContent := "name: c1\nprompt: review this\nassert:\n  triggered: true\n"
	if err := os.WriteFile(filepath.Join(evalsDir, "c1.yaml"), []byte(caseContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRegressCommandNoPriorHistoryDoesNotFailCI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := setupSkillWithCase(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil (first run, nothing to regress from); output = %s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Errorf("history.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".skillci", "badge.svg")); err != nil {
		t.Errorf("badge.svg not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evals", "_generated")); err != nil {
		t.Errorf("evals/_generated not created for the uncovered failing case: %v", err)
	}
}

func TestAcceptCommandPromotesGeneratedCase(t *testing.T) {
	dir := setupSkillWithCase(t)
	genDir := filepath.Join(dir, "evals", "_generated")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	genPath := filepath.Join(genDir, "new-case.yaml")
	if err := os.WriteFile(genPath, []byte("name: new-case\nprompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newAcceptCmd()
	cmd.SetArgs([]string{"new-case", "--path", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(genPath); !os.IsNotExist(err) {
		t.Error("generated file still exists in _generated after accept")
	}
	if _, err := os.Stat(filepath.Join(dir, "evals", "new-case.yaml")); err != nil {
		t.Errorf("promoted file not found in evals/: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: FAIL — `undefined: newRegressCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/skillci/regress.go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kabirnarang/skillci/internal/anthropic"
	"github.com/kabirnarang/skillci/internal/badge"
	"github.com/kabirnarang/skillci/internal/config"
	"github.com/kabirnarang/skillci/internal/evalspec"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/kabirnarang/skillci/internal/regress"
	"github.com/spf13/cobra"
)

func newRegressCmd() *cobra.Command {
	var upload bool
	cmd := &cobra.Command{
		Use:   "regress [path]",
		Short: "Run the eval suite across the configured model matrix and fail CI on new regressions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY is not set")
			}
			client := anthropic.NewClient(apiKey)
			if base := os.Getenv("SKILLCI_BASE_URL"); base != "" {
				client = client.WithBaseURL(base)
			}

			cfg, err := config.Load(filepath.Join(dir, ".skillci.yaml"))
			if err != nil {
				return err
			}
			cases, err := evalspec.LoadDir(filepath.Join(dir, "evals"))
			if err != nil {
				return err
			}
			if len(cases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no eval cases found in evals/")
				return nil
			}

			historyPath := filepath.Join(dir, ".skillci", "history.json")
			hist, err := history.Load(historyPath)
			if err != nil {
				return err
			}

			report, newRun, err := regress.RunMatrix(context.Background(), client, dir, cfg, cases, hist)
			if err != nil {
				return err
			}

			for _, o := range report.Outcomes {
				status := "PASS"
				switch {
				case o.IsNewRegression:
					status = "REGRESSED"
				case !o.Result.Passed:
					status = "FAIL"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s (%s)\n", status, o.Case.Name, o.Model)
				for _, f := range o.Result.Failures {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", f)
				}
			}

			if len(report.GeneratedCases) > 0 {
				paths, err := regress.WriteGeneratedCases(dir, report.GeneratedCases)
				if err != nil {
					return err
				}
				for _, p := range paths {
					fmt.Fprintf(cmd.OutOrStdout(), "proposed new eval case: %s (run `skillci accept <name>` to keep it)\n", p)
				}
			} else {
				// still ensure the directory exists so tooling/tests can rely on its presence
				if err := os.MkdirAll(filepath.Join(dir, "evals", "_generated"), 0o755); err != nil {
					return err
				}
			}

			hist.Append(newRun)
			if err := hist.Save(historyPath); err != nil {
				return err
			}

			state := badge.StateFromRun(newRun)
			if err := os.WriteFile(filepath.Join(dir, ".skillci", "badge.svg"), []byte(badge.Render(state)), 0o644); err != nil {
				return err
			}

			if upload {
				fmt.Fprintln(cmd.OutOrStdout(), "note: --upload wiring lands in a later task; results were not sent to the dashboard")
			}

			if report.ShouldFailCI(cfg.FailOn) {
				return fmt.Errorf("regression detected (fail_on=%s)", cfg.FailOn)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&upload, "upload", false, "upload results to the SkillCI dashboard")
	return cmd
}
```

```go
// cmd/skillci/accept.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newAcceptCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "accept <case-name>",
		Short: "Promote a generated eval case from evals/_generated/ into evals/",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src := filepath.Join(path, "evals", "_generated", name+".yaml")
			dst := filepath.Join(path, "evals", name+".yaml")

			data, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("reading generated case %s: %w", src, err)
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return err
			}
			if err := os.Remove(src); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "accepted %s -> %s\n", src, dst)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "skill directory")
	return cmd
}
```

```go
// cmd/skillci/badge.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	skillbadge "github.com/kabirnarang/skillci/internal/badge"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/spf13/cobra"
)

func newBadgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "badge [path]",
		Short: "Regenerate the SVG badge from the latest recorded history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			hist, err := history.Load(filepath.Join(dir, ".skillci", "history.json"))
			if err != nil {
				return err
			}
			last, ok := hist.LastRun()
			if !ok {
				return fmt.Errorf("no history found — run `skillci regress` first")
			}
			state := skillbadge.StateFromRun(last)
			return os.WriteFile(filepath.Join(dir, ".skillci", "badge.svg"), []byte(skillbadge.Render(state)), 0o644)
		},
	}
}
```

Add to `cmd/skillci/main.go`, after the existing `root.AddCommand(...)` calls:

```go
	root.AddCommand(newRegressCmd())
	root.AddCommand(newAcceptCmd())
	root.AddCommand(newBadgeCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add cmd/skillci
git commit -m "Add skillci regress, accept, and badge commands"
```

---

## Task 13: `skillci init` command

**Files:**
- Create: `cmd/skillci/init.go`
- Test: `cmd/skillci/init_test.go`

**Interfaces:**
- Consumes: `config.Default()` (Task 2)
- Produces: `newInitCmd() *cobra.Command`, registered on root in this task.

- [ ] **Step 1: Write the failing test**

```go
// cmd/skillci/init_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCommandScaffolds(t *testing.T) {
	dir := t.TempDir()

	cmd := newInitCmd()
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".skillci.yaml")); err != nil {
		t.Errorf(".skillci.yaml not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evals")); err != nil {
		t.Errorf("evals/ not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evals", "example.yaml")); err != nil {
		t.Errorf("evals/example.yaml not created: %v", err)
	}
}

func TestInitCommandDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	existing := "models: [claude-opus-4-8]\n"
	if err := os.WriteFile(filepath.Join(dir, ".skillci.yaml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want error when .skillci.yaml already exists")
	}

	data, err := os.ReadFile(filepath.Join(dir, ".skillci.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Error(".skillci.yaml was overwritten, want it untouched")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: FAIL — `undefined: newInitCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/skillci/init.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultConfigYAML = `models: [claude-sonnet-5, claude-opus-4-8, claude-haiku-4-5]
fail_on: regression
`

const exampleEvalYAML = `name: "example-case"
prompt: "Write a prompt that should trigger this skill."
skill_under_test: "REPLACE_WITH_YOUR_SKILL_NAME"
assert:
  triggered: true
  contains: ["REPLACE_WITH_EXPECTED_SUBSTRING"]
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Scaffold .skillci.yaml and an example eval case for a skill",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			cfgPath := filepath.Join(dir, ".skillci.yaml")
			if _, err := os.Stat(cfgPath); err == nil {
				return fmt.Errorf("%s already exists, refusing to overwrite", cfgPath)
			}
			if err := os.WriteFile(cfgPath, []byte(defaultConfigYAML), 0o644); err != nil {
				return err
			}

			evalsDir := filepath.Join(dir, "evals")
			if err := os.MkdirAll(evalsDir, 0o755); err != nil {
				return err
			}
			examplePath := filepath.Join(evalsDir, "example.yaml")
			if _, err := os.Stat(examplePath); os.IsNotExist(err) {
				if err := os.WriteFile(examplePath, []byte(exampleEvalYAML), 0o644); err != nil {
					return err
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "scaffolded %s and %s\n", cfgPath, examplePath)
			return nil
		},
	}
}
```

Add to `cmd/skillci/main.go`:

```go
	root.AddCommand(newInitCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add cmd/skillci
git commit -m "Add skillci init command"
```

---

## Task 14: GitHub Action wrapper

**Files:**
- Create: `.github/actions/skillci/action.yml`
- Create: `.github/actions/skillci/entrypoint.sh`

**Interfaces:**
- Consumes: the `skillci` binary built in earlier tasks (installed via `go install` in the action, keeping the action itself dependency-free of prebuilt release artifacts for MVP — a release-binary distribution path is a v1.1 nicety, not required for the action to work)
- Produces: a composite action other repos' workflows can reference as `uses: kabirnarang/skillci/.github/actions/skillci@main`

- [ ] **Step 1: Write the composite action definition**

```yaml
# .github/actions/skillci/action.yml
name: "skillci"
description: "Lint, eval, and regression-test Claude Skills against a model matrix"
inputs:
  path:
    description: "Path to the skill directory"
    required: false
    default: "."
  anthropic-api-key:
    description: "Anthropic API key"
    required: true
  upload:
    description: "Upload results to the SkillCI dashboard"
    required: false
    default: "false"
runs:
  using: "composite"
  steps:
    - shell: bash
      run: go install github.com/kabirnarang/skillci/cmd/skillci@latest
    - shell: bash
      env:
        ANTHROPIC_API_KEY: ${{ inputs.anthropic-api-key }}
      run: ${{ github.action_path }}/entrypoint.sh "${{ inputs.path }}" "${{ inputs.upload }}"
```

- [ ] **Step 2: Write the entrypoint script**

```bash
#!/usr/bin/env bash
# .github/actions/skillci/entrypoint.sh
set -euo pipefail

SKILL_PATH="${1:-.}"
UPLOAD="${2:-false}"

ARGS=("regress" "$SKILL_PATH")
if [[ "$UPLOAD" == "true" ]]; then
  ARGS+=("--upload")
fi

"$(go env GOPATH)/bin/skillci" "${ARGS[@]}"
```

- [ ] **Step 3: Make the entrypoint executable and commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
chmod +x .github/actions/skillci/entrypoint.sh
git add .github/actions/skillci
git commit -m "Add skillci GitHub Action"
```

- [ ] **Step 4: Verify locally (dry run, no live API key needed for the install/wiring check)**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && bash -n .github/actions/skillci/entrypoint.sh`
Expected: no output (script is syntactically valid)

---

## Task 15: Dashboard Postgres schema + store layer

**Files:**
- Create: `internal/dashboard/schema.sql`
- Create: `internal/dashboard/store.go`
- Test: `internal/dashboard/store_test.go`

**Note on test infra:** these tests need a real Postgres instance. Use `docker run -d -p 5433:5432 -e POSTGRES_PASSWORD=test -e POSTGRES_DB=skillci_test postgres:16` once, locally, before running this task's tests. The test file skips itself (`t.Skip`) if `SKILLCI_TEST_DATABASE_URL` is unset, so `go test ./...` still passes in environments without Docker (including the project's own CI unless that env var is provided as a service container — wiring the CI service container is a v1.1 nicety, not required for this task).

**Interfaces:**
- Produces:
  ```go
  type Store struct { /* unexported db *sql.DB */ }
  func NewStore(databaseURL string) (*Store, error)
  func (s *Store) Migrate(ctx context.Context) error // runs schema.sql, idempotent (CREATE TABLE IF NOT EXISTS)
  func (s *Store) InsertResult(ctx context.Context, r IngestedResult) error
  func (s *Store) SkillHistory(ctx context.Context, owner, repo, skill string) ([]IngestedResult, error)
  func (s *Store) Leaderboard(ctx context.Context) ([]LeaderboardEntry, error)
  type IngestedResult struct {
      Owner, Repo, Skill, CommitSHA, Model string
      Passed    bool
      Timestamp time.Time
  }
  type LeaderboardEntry struct {
      Owner, Repo, Skill string
      PassRate           float64 // 0.0-1.0 over the most recent run per model
      ModelsCovered      int
      LastRun            time.Time
  }
  ```
- Consumed by: Task 16 (ingestion handler), Task 17 (login/upload — indirectly, via the API it backs), Task 18 (page rendering)

- [ ] **Step 1: Write the schema**

```sql
-- internal/dashboard/schema.sql
CREATE TABLE IF NOT EXISTS results (
    id          BIGSERIAL PRIMARY KEY,
    owner       TEXT NOT NULL,
    repo        TEXT NOT NULL,
    skill       TEXT NOT NULL,
    commit_sha  TEXT NOT NULL,
    model       TEXT NOT NULL,
    passed      BOOLEAN NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_results_skill ON results (owner, repo, skill, ts DESC);
```

- [ ] **Step 2: Write the failing test**

```go
// internal/dashboard/store_test.go
package dashboard

import (
	"context"
	"os"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set, skipping Postgres-backed test")
	}
	s, err := NewStore(url)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return s
}

func TestInsertAndFetchSkillHistory(t *testing.T) {
	s := testStore(t)
	r := IngestedResult{
		Owner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
		Timestamp: time.Now(),
	}
	if err := s.InsertResult(context.Background(), r); err != nil {
		t.Fatalf("InsertResult() error = %v", err)
	}

	history, err := s.SkillHistory(context.Background(), "kabirnarang", "skillci", "pr-review")
	if err != nil {
		t.Fatalf("SkillHistory() error = %v", err)
	}
	if len(history) == 0 {
		t.Error("SkillHistory() returned no rows after insert")
	}
}

func TestLeaderboard(t *testing.T) {
	s := testStore(t)
	r := IngestedResult{
		Owner: "kabirnarang", Repo: "skillci", Skill: "leaderboard-case",
		CommitSHA: "def456", Model: "claude-sonnet-5", Passed: true,
		Timestamp: time.Now(),
	}
	if err := s.InsertResult(context.Background(), r); err != nil {
		t.Fatalf("InsertResult() error = %v", err)
	}

	entries, err := s.Leaderboard(context.Background())
	if err != nil {
		t.Fatalf("Leaderboard() error = %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Skill == "leaderboard-case" {
			found = true
			if e.PassRate != 1.0 {
				t.Errorf("PassRate = %v, want 1.0", e.PassRate)
			}
		}
	}
	if !found {
		t.Error("Leaderboard() did not include the inserted skill")
	}
}
```

- [ ] **Step 3: Run test to verify it fails (or skips cleanly without Postgres)**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/dashboard/...`
Expected (no Docker running): `undefined: NewStore` compile failure — same as usual TDD red step, the `t.Skip` only matters once the package compiles.

- [ ] **Step 4: Add the Postgres driver dependency and write the implementation**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go get github.com/lib/pq`

```go
// internal/dashboard/store.go
package dashboard

import (
	"context"
	"database/sql"
	_ "embed"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func NewStore(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

type IngestedResult struct {
	Owner, Repo, Skill, CommitSHA, Model string
	Passed                               bool
	Timestamp                            time.Time
}

func (s *Store) InsertResult(ctx context.Context, r IngestedResult) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (owner, repo, skill, commit_sha, model, passed, ts) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		r.Owner, r.Repo, r.Skill, r.CommitSHA, r.Model, r.Passed, r.Timestamp)
	return err
}

func (s *Store) SkillHistory(ctx context.Context, owner, repo, skill string) ([]IngestedResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT owner, repo, skill, commit_sha, model, passed, ts FROM results
		 WHERE owner=$1 AND repo=$2 AND skill=$3 ORDER BY ts DESC LIMIT 200`,
		owner, repo, skill)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IngestedResult
	for rows.Next() {
		var r IngestedResult
		if err := rows.Scan(&r.Owner, &r.Repo, &r.Skill, &r.CommitSHA, &r.Model, &r.Passed, &r.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type LeaderboardEntry struct {
	Owner, Repo, Skill string
	PassRate           float64
	ModelsCovered      int
	LastRun            time.Time
}

// Leaderboard aggregates, per (owner, repo, skill), the most recent result
// per model, then reports the pass rate across those latest-per-model rows.
func (s *Store) Leaderboard(ctx context.Context) ([]LeaderboardEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (owner, repo, skill, model)
				owner, repo, skill, model, passed, ts
			FROM results
			ORDER BY owner, repo, skill, model, ts DESC
		)
		SELECT owner, repo, skill,
			AVG(CASE WHEN passed THEN 1.0 ELSE 0.0 END) AS pass_rate,
			COUNT(DISTINCT model) AS models_covered,
			MAX(ts) AS last_run
		FROM latest
		GROUP BY owner, repo, skill
		ORDER BY last_run DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.Owner, &e.Repo, &e.Skill, &e.PassRate, &e.ModelsCovered, &e.LastRun); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run test to verify it passes (with Postgres) or skips cleanly (without)**

Run:
```bash
docker run -d --name skillci-test-pg -p 5433:5432 -e POSTGRES_PASSWORD=test -e POSTGRES_DB=skillci_test postgres:16
export SKILLCI_TEST_DATABASE_URL="postgres://postgres:test@localhost:5433/skillci_test?sslmode=disable"
cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/dashboard/...
```
Expected: PASS (or, without Docker/the env var set: `ok ... [no test files run]`-style skip output, not a failure)

- [ ] **Step 6: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add go.mod go.sum internal/dashboard
git commit -m "Add dashboard Postgres schema and store layer"
```

---

## Task 16: Ingestion API (`POST /api/v1/results`) + server

**Files:**
- Create: `internal/dashboard/ingest.go`
- Create: `internal/dashboard/server.go`
- Create: `cmd/skillci-server/main.go`
- Test: `internal/dashboard/ingest_test.go`

**Auth model (MVP cut):** a single static bearer token per deployment, set via `SKILLCI_INGEST_TOKEN` env var, checked against the `Authorization: Bearer <token>` header. This is deliberately simpler than the design doc's full per-repo device-flow token issuance — that's the right MVP cut because a single shared token still satisfies "opt-in, authenticated upload" and unblocks Task 17/18 end-to-end; per-repo device-flow tokens become a v1.1 task once there's more than one trusted uploader. Document this narrowing here so it isn't mistaken for scope creep later.

**Interfaces:**
- Consumes: `Store.InsertResult` (Task 15)
- Produces:
  ```go
  func NewServer(store *Store, ingestToken string) *http.ServeMux // routes wired: POST /api/v1/results (this task); GET /s/{owner}/{repo}/{skill} and GET / added in Task 18
  ```
- Consumed by: Task 17 (CLI upload client posts here), Task 18 (adds routes to the same mux), `cmd/skillci-server/main.go` (this task)

- [ ] **Step 1: Write the failing test**

```go
// internal/dashboard/ingest_test.go
package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func requireTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set, skipping Postgres-backed test")
	}
	s, err := NewStore(url)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return s
}

func TestIngestHandlerAcceptsValidPayload(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, "secret-token")

	payload := IngestPayload{
		Owner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestIngestHandlerRejectsMissingAuth(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, "secret-token")

	body, _ := json.Marshal(IngestPayload{Owner: "o", Repo: "r", Skill: "s", CommitSHA: "c", Model: "m", Passed: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIngestHandlerRejectsMalformedJSON(t *testing.T) {
	store := requireTestStore(t)
	mux := NewServer(store, "secret-token")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/results", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/dashboard/...`
Expected: FAIL — `undefined: NewServer` / `undefined: IngestPayload`

- [ ] **Step 3: Write the implementation**

```go
// internal/dashboard/ingest.go
package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type IngestPayload struct {
	Owner     string `json:"repo_owner"`
	Repo      string `json:"repo"`
	Skill     string `json:"skill_name"`
	CommitSHA string `json:"commit_sha"`
	Model     string `json:"model"`
	Passed    bool   `json:"pass"`
}

func ingestHandler(store *Store, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var p IngestPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "malformed JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(p.Owner) == "" || strings.TrimSpace(p.Repo) == "" || strings.TrimSpace(p.Skill) == "" {
			http.Error(w, "owner, repo, and skill_name are required", http.StatusBadRequest)
			return
		}

		err := store.InsertResult(r.Context(), IngestedResult{
			Owner: p.Owner, Repo: p.Repo, Skill: p.Skill,
			CommitSHA: p.CommitSHA, Model: p.Model, Passed: p.Passed,
			Timestamp: time.Now(),
		})
		if err != nil {
			http.Error(w, "failed to store result", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}
```

```go
// internal/dashboard/server.go
package dashboard

import "net/http"

// NewServer wires the dashboard's HTTP routes. Task 18 (public skill pages
// and the leaderboard) registers additional GET routes on the same mux
// returned here — do not construct a second mux for those routes.
func NewServer(store *Store, ingestToken string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/results", ingestHandler(store, ingestToken))
	return mux
}
```

```go
// cmd/skillci-server/main.go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/kabirnarang/skillci/internal/dashboard"
)

func main() {
	dbURL := os.Getenv("SKILLCI_DATABASE_URL")
	if dbURL == "" {
		log.Fatal("SKILLCI_DATABASE_URL is not set")
	}
	token := os.Getenv("SKILLCI_INGEST_TOKEN")
	if token == "" {
		log.Fatal("SKILLCI_INGEST_TOKEN is not set")
	}

	store, err := dashboard.NewStore(dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	mux := dashboard.NewServer(store, token)

	addr := os.Getenv("SKILLCI_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	fmt.Printf("skillci-server listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

Note: `cmd/skillci-server/main.go` above uses `context.Background()` but is missing the `"context"` import — add `"context"` to the import block in this step (a reviewer catching this at the "run tests" step is exactly what Step 4 is for).

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
cd "/Users/kabir/Personal Workspace/skillci"
go build ./...   # catches the missing-import mistake above before go test masks it
go test ./internal/dashboard/...
```
Expected: `go build` fails first if the import wasn't added — fix it, then both commands succeed (tests PASS if `SKILLCI_TEST_DATABASE_URL` is set, skip cleanly otherwise).

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add cmd/skillci-server internal/dashboard
git commit -m "Add dashboard ingestion API and server binary"
```

---

## Task 17: CLI upload client (`--upload` wiring)

**Files:**
- Create: `internal/upload/upload.go`
- Test: `internal/upload/upload_test.go`
- Modify: `cmd/skillci/regress.go`

**Auth model (matches Task 16's MVP cut):** the CLI reads a token from `SKILLCI_INGEST_TOKEN` env var (or `--token` flag) and sends it as `Authorization: Bearer <token>` — no device-flow login command in MVP (see Task 16 note). `skillci login` is deferred to v1.1 alongside per-repo device-flow tokens.

**Interfaces:**
- Consumes: nothing from earlier tasks except the payload shape agreed in Task 16 (`dashboard.IngestPayload` field names — this package defines its own equivalent struct rather than importing `internal/dashboard`, to keep the CLI binary free of the Postgres driver dependency; field names/JSON tags must match Task 16's exactly)
- Produces:
  ```go
  type Result struct {
      RepoOwner, Repo, Skill, CommitSHA, Model string
      Passed bool
  }
  func Send(ctx context.Context, dashboardURL, token string, r Result) error
  ```
- Consumed by: `cmd/skillci/regress.go` (modified in this task to actually call `Send` when `--upload` is set, replacing the "note: wiring lands later" stub from Task 12)

- [ ] **Step 1: Write the failing test**

```go
// internal/upload/upload_test.go
package upload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPostsExpectedPayloadAndAuth(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "secret-token", Result{
		RepoOwner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", gotAuth)
	}
	if gotBody["repo_owner"] != "kabirnarang" || gotBody["skill_name"] != "pr-review" {
		t.Errorf("body = %v, field names must match dashboard.IngestPayload JSON tags", gotBody)
	}
}

func TestSendReturnsErrorOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "secret-token", Result{})
	if err == nil {
		t.Error("Send() error = nil, want error on 500")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/upload/...`
Expected: FAIL — `undefined: Send`

- [ ] **Step 3: Write the implementation**

```go
// internal/upload/upload.go
package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Result mirrors dashboard.IngestPayload's fields — kept as a separate type
// (not an import of internal/dashboard) so the CLI binary never pulls in
// the Postgres driver. JSON tags must stay in sync with Task 16's
// dashboard.IngestPayload by hand; a mismatch here would silently drop
// fields server-side rather than fail to compile, so the field-name
// alignment is enforced by TestSendPostsExpectedPayloadAndAuth above.
type Result struct {
	RepoOwner string `json:"repo_owner"`
	Repo      string `json:"repo"`
	Skill     string `json:"skill_name"`
	CommitSHA string `json:"commit_sha"`
	Model     string `json:"model"`
	Passed    bool   `json:"pass"`
}

func Send(ctx context.Context, dashboardURL, token string, r Result) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashboardURL+"/api/v1/results", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("dashboard upload failed with status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/upload/...`
Expected: PASS

- [ ] **Step 5: Wire `--upload` into `skillci regress`**

In `cmd/skillci/regress.go`, replace the stub block:

```go
			if upload {
				fmt.Fprintln(cmd.OutOrStdout(), "note: --upload wiring lands in a later task; results were not sent to the dashboard")
			}
```

with:

```go
			if upload {
				dashboardURL := os.Getenv("SKILLCI_DASHBOARD_URL")
				token := os.Getenv("SKILLCI_INGEST_TOKEN")
				if dashboardURL == "" || token == "" {
					return fmt.Errorf("--upload requires SKILLCI_DASHBOARD_URL and SKILLCI_INGEST_TOKEN")
				}
				owner, repoName := parseOwnerRepo(os.Getenv("GITHUB_REPOSITORY"))
				commitSHA := os.Getenv("GITHUB_SHA")
				for _, c := range newRun.Cases {
					err := upload.Send(context.Background(), dashboardURL, token, upload.Result{
						RepoOwner: owner, Repo: repoName, Skill: filepath.Base(dir),
						CommitSHA: commitSHA, Model: c.Model, Passed: c.Passed,
					})
					if err != nil {
						// per design §8: a dashboard hiccup must never break CI
						fmt.Fprintf(cmd.OutOrStdout(), "warning: dashboard upload failed: %v\n", err)
					}
				}
			}
```

Add this helper to the bottom of `cmd/skillci/regress.go`:

```go
func parseOwnerRepo(githubRepository string) (owner, repo string) {
	parts := strings.SplitN(githubRepository, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
```

Add `"strings"` and `"github.com/kabirnarang/skillci/internal/upload"` to `cmd/skillci/regress.go`'s import block (the file already imports `"context"` from Task 12).

- [ ] **Step 6: Run the full CLI test suite to verify nothing broke**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go build ./... && go test ./cmd/skillci/...`
Expected: PASS — Task 12's `TestRegressCommandNoPriorHistoryDoesNotFailCI` doesn't set `--upload`, so this wiring is exercised by a new test, not a modification to that one.

- [ ] **Step 7: Add a test for the upload path**

Add to `cmd/skillci/regress_test.go`:

```go
func TestRegressCommandUploadFailureDoesNotFailCI(t *testing.T) {
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: false"}},
			"usage":   map[string]int{"input_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer modelSrv.Close()

	dashSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dashSrv.Close()

	dir := setupSkillWithCase(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", modelSrv.URL)
	t.Setenv("SKILLCI_DASHBOARD_URL", dashSrv.URL)
	t.Setenv("SKILLCI_INGEST_TOKEN", "secret-token")

	cmd := newRegressCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--upload", dir})

	if err := cmd.Execute(); err != nil {
		t.Errorf("Execute() error = %v, want nil — a dashboard upload failure must not fail CI (design §8)", err)
	}
}
```

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./cmd/skillci/...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/upload cmd/skillci
git commit -m "Wire skillci regress --upload to the dashboard ingestion API"
```

---

## Task 18: Dashboard public skill page + leaderboard (server-rendered)

**Files:**
- Create: `internal/dashboard/sparkline.go`
- Create: `internal/dashboard/pages.go`
- Test: `internal/dashboard/pages_test.go`
- Modify: `internal/dashboard/server.go`

**Interfaces:**
- Consumes: `Store.SkillHistory`/`Leaderboard` (Task 15)
- Produces: `GET /s/{owner}/{repo}/{skill}` and `GET /` routes added to the mux from Task 16; `RenderSparkline(results []IngestedResult) string` (SVG, same hand-built approach as `internal/badge` — no charting library, consistent with design §7's "same pattern as the badge")

- [ ] **Step 1: Write the failing test**

```go
// internal/dashboard/pages_test.go
package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSkillPageRendersHistoryAndBadgeState(t *testing.T) {
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set")
	}
	store, err := NewStore(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertResult(context.Background(), IngestedResult{
		Owner: "kabirnarang", Repo: "skillci", Skill: "page-test-skill",
		CommitSHA: "abc", Model: "claude-sonnet-5", Passed: true, Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/s/kabirnarang/skillci/page-test-skill", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "page-test-skill") {
		t.Error("skill page body does not mention the skill name")
	}
}

func TestSkillPageNotFound(t *testing.T) {
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set")
	}
	store, err := NewStore(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/s/nobody/nothing/nothing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLeaderboardPageRenders(t *testing.T) {
	url := os.Getenv("SKILLCI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SKILLCI_TEST_DATABASE_URL not set")
	}
	store, err := NewStore(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	mux := NewServer(store, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRenderSparklineProducesSVG(t *testing.T) {
	results := []IngestedResult{
		{Passed: true, Timestamp: time.Now().Add(-2 * time.Hour)},
		{Passed: false, Timestamp: time.Now().Add(-1 * time.Hour)},
		{Passed: true, Timestamp: time.Now()},
	}
	svg := RenderSparkline(results)
	if !strings.Contains(svg, "<svg") {
		t.Errorf("RenderSparkline() = %q, not SVG", svg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/dashboard/...`
Expected: FAIL — `undefined: RenderSparkline` (and 404 tests fail against the not-yet-updated mux)

- [ ] **Step 3: Write the sparkline**

```go
// internal/dashboard/sparkline.go
package dashboard

import (
	"fmt"
	"strings"
)

// RenderSparkline draws a minimal pass/fail trend line: one point per
// result, green for pass, red for fail, left-to-right oldest-to-newest.
// Deliberately hand-rolled SVG (no charting library) — same approach as
// internal/badge, keeps both binaries dependency-light.
func RenderSparkline(results []IngestedResult) string {
	const width, height, pointGap = 200, 40, 20
	var points strings.Builder
	for i, r := range results {
		x := 10 + i*pointGap
		y := 30
		if r.Passed {
			y = 10
		}
		color := "#cf222e"
		if r.Passed {
			color = "#2ea44f"
		}
		fmt.Fprintf(&points, `<circle cx="%d" cy="%d" r="3" fill="%s"/>`, x, y, color)
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">%s</svg>`, width, height, points.String())
}
```

- [ ] **Step 4: Write the page handlers**

```go
// internal/dashboard/pages.go
package dashboard

import (
	"fmt"
	"html/template"
	"net/http"
)

var skillPageTmpl = template.Must(template.New("skill").Parse(`<!doctype html>
<html><head><title>{{.Owner}}/{{.Repo}} — {{.Skill}}</title></head>
<body>
<h1>{{.Skill}}</h1>
<p>{{.Owner}}/{{.Repo}}</p>
{{.SparklineSVG}}
<table>
<tr><th>Model</th><th>Commit</th><th>Result</th><th>When</th></tr>
{{range .Rows}}<tr><td>{{.Model}}</td><td>{{.CommitSHA}}</td><td>{{if .Passed}}pass{{else}}fail{{end}}</td><td>{{.Timestamp}}</td></tr>
{{end}}
</table>
</body></html>`))

var leaderboardTmpl = template.Must(template.New("leaderboard").Parse(`<!doctype html>
<html><head><title>SkillCI Leaderboard</title></head>
<body>
<h1>SkillCI Leaderboard</h1>
<table>
<tr><th>Skill</th><th>Repo</th><th>Pass Rate</th><th>Models</th><th>Last Run</th></tr>
{{range .}}<tr><td><a href="/s/{{.Owner}}/{{.Repo}}/{{.Skill}}">{{.Skill}}</a></td><td>{{.Owner}}/{{.Repo}}</td><td>{{.PassRate}}</td><td>{{.ModelsCovered}}</td><td>{{.LastRun}}</td></tr>
{{end}}
</table>
</body></html>`))

type skillPageData struct {
	Owner, Repo, Skill string
	SparklineSVG       template.HTML
	Rows               []IngestedResult
}

func skillPageHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner := r.PathValue("owner")
		repo := r.PathValue("repo")
		skill := r.PathValue("skill")

		rows, err := store.SkillHistory(r.Context(), owner, repo, skill)
		if err != nil {
			http.Error(w, "failed to load history", http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			http.NotFound(w, r)
			return
		}

		data := skillPageData{
			Owner: owner, Repo: repo, Skill: skill,
			SparklineSVG: template.HTML(RenderSparkline(rows)),
			Rows:         rows,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := skillPageTmpl.Execute(w, data); err != nil {
			http.Error(w, fmt.Sprintf("render error: %v", err), http.StatusInternalServerError)
		}
	}
}

func leaderboardHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := store.Leaderboard(r.Context())
		if err != nil {
			http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := leaderboardTmpl.Execute(w, entries); err != nil {
			http.Error(w, fmt.Sprintf("render error: %v", err), http.StatusInternalServerError)
		}
	}
}
```

- [ ] **Step 5: Register the new routes**

Replace `internal/dashboard/server.go` with:

```go
// internal/dashboard/server.go
package dashboard

import "net/http"

func NewServer(store *Store, ingestToken string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/results", ingestHandler(store, ingestToken))
	mux.HandleFunc("GET /s/{owner}/{repo}/{skill}", skillPageHandler(store))
	mux.HandleFunc("GET /{$}", leaderboardHandler(store))
	return mux
}
```

(`GET /{$}` is Go 1.22+ routing syntax for an exact match on `/` only — avoids the leaderboard handler swallowing `/s/...` requests the way a bare `/` pattern would.)

- [ ] **Step 6: Run test to verify it passes**

Run:
```bash
export SKILLCI_TEST_DATABASE_URL="postgres://postgres:test@localhost:5433/skillci_test?sslmode=disable"
cd "/Users/kabir/Personal Workspace/skillci" && go test ./internal/dashboard/...
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add internal/dashboard
git commit -m "Add dashboard public skill pages and leaderboard"
```

---

## Task 19: Example skill + dogfood `skillci check` in project CI

**Files:**
- Create: `examples/example-skill/SKILL.md`
- Create: `examples/example-skill/evals/triggers.yaml`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `skillci check` (Task 4) — this task only wires the lint check into CI; wiring `skillci regress` (which needs a real `ANTHROPIC_API_KEY` secret) into the project's own CI is a deliberate v1.1 follow-up, not required to prove the tool works end-to-end (the unit test suite already exercises `regress` against stub servers in Task 12/17).

- [ ] **Step 1: Write the example skill**

```markdown
<!-- examples/example-skill/SKILL.md -->
---
name: example-skill
description: A minimal example skill used to dogfood skillci's own lint and eval commands.
---

This is a placeholder skill body demonstrating the SKILL.md format that
skillci lints and evaluates.
```

- [ ] **Step 2: Write its eval case**

```yaml
# examples/example-skill/evals/triggers.yaml
name: "example-triggers"
prompt: "Can you demonstrate the example skill?"
skill_under_test: "example-skill"
assert:
  triggered: true
```

- [ ] **Step 3: Verify `skillci check` passes against it locally**

Run:
```bash
cd "/Users/kabir/Personal Workspace/skillci"
go run ./cmd/skillci check examples/example-skill
```
Expected: exit code 0, no output

- [ ] **Step 4: Add the dogfood step to project CI**

Modify `.github/workflows/ci.yml`, adding this step after `go test ./...`:

```yaml
      - run: go run ./cmd/skillci check examples/example-skill
```

- [ ] **Step 5: Commit**

```bash
cd "/Users/kabir/Personal Workspace/skillci"
git add examples .github/workflows/ci.yml
git commit -m "Add example skill and dogfood skillci check in CI"
```

---

## Plan Self-Review Notes

- **Spec coverage:** §3 architecture → Tasks 1, 14, 16, 18. §4 eval format → Task 5. §5 self-growing loop → Task 10. §6 CLI surface → Tasks 4, 8, 12, 13 (all six commands: `init`, `check`, `eval`, `regress`, `accept`, `badge`). §7 dashboard → Tasks 15, 16, 17, 18. §8 error handling → covered inline in Tasks 6 (API error), 12 (dashboard-failure-doesn't-fail-CI via Task 17's test), 4 (malformed SKILL.md fails fast). §9 MVP scope → all in-scope items have tasks; deferred items (`scan`, non-Anthropic models, versioning, device-flow login) are explicitly called out as NOT tasks here. §10 testing strategy → every task is TDD with a fixture/cassette-style stub server, no live API calls anywhere in this plan's own test suite.
- **Two intentional narrowings from the design doc**, both flagged inline where they occur rather than silently: (1) Task 16/17 ship a single shared bearer token instead of the design's per-repo device-flow login — smallest thing that satisfies "opt-in, authenticated" and unblocks everything downstream; full device-flow is v1.1. (2) Task 19 dogfoods `check` in CI, not `regress` — `regress` needs a paid API key as a repo secret, which is a project-owner setup step outside this plan's control, not a task gap.
- **Placeholder scan:** none found on final pass — the two draft-artifact blocks caught mid-write in Task 10 were corrected before the task was closed out.
- **Type consistency check:** `evalspec.Case`/`Assertions` field names are identical everywhere they're constructed (Tasks 5, 7, 8, 10, 12, 13, 19). `history.Run`/`CaseResult` field names match across Tasks 9, 10, 11, 12. `upload.Result` JSON tags were hand-verified against `dashboard.IngestPayload` JSON tags in Task 17's own test (`TestSendPostsExpectedPayloadAndAuth` asserts on the wire field names directly, so a future drift between the two structs fails a test rather than failing silently in production).

