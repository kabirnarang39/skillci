package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestScaffoldRedteamPluginDeterministicIsValidGo(t *testing.T) {
	var buf bytes.Buffer
	cmd := newScaffoldCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"redteam-plugin", "my-attack", "--grading", "deterministic"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "BuildAttack") || !strings.Contains(out, "Detect") {
		t.Errorf("output = %q, want a deterministic-shaped stub with BuildAttack/Detect", out)
	}
	if strings.Contains(out, "JudgeRubric") {
		t.Errorf("output = %q, want no JudgeRubric field in a deterministic stub", out)
	}
	assertValidGoSnippet(t, out)
}

func TestScaffoldRedteamPluginJudgeIsValidGo(t *testing.T) {
	var buf bytes.Buffer
	cmd := newScaffoldCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"redteam-plugin", "my-attack", "--grading", "judge"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "AttackPromptSuffix") || !strings.Contains(out, "JudgeRubric") {
		t.Errorf("output = %q, want a judge-graded-shaped stub with AttackPromptSuffix/JudgeRubric", out)
	}
	if strings.Contains(out, "BuildAttack") {
		t.Errorf("output = %q, want no BuildAttack field in a judge-graded stub", out)
	}
	assertValidGoSnippet(t, out)
}

func TestScaffoldRedteamPluginMultiturnIsValidGo(t *testing.T) {
	var buf bytes.Buffer
	cmd := newScaffoldCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"redteam-plugin", "my-attack", "--grading", "multiturn"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Turns:") || !strings.Contains(out, "TurnStep") {
		t.Errorf("output = %q, want a multi-turn-shaped stub with Turns/TurnStep", out)
	}
	assertValidGoSnippet(t, out)
}

func TestScaffoldRedteamPluginRejectsUnknownGrading(t *testing.T) {
	var buf bytes.Buffer
	cmd := newScaffoldCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"redteam-plugin", "my-attack", "--grading", "bogus"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want an error for an unrecognized --grading value")
	}
}

func TestScaffoldRedteamPluginUsesNameInStub(t *testing.T) {
	var buf bytes.Buffer
	cmd := newScaffoldCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"redteam-plugin", "session-hijack-probe", "--grading", "deterministic"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(buf.String(), "session-hijack-probe") {
		t.Errorf("output = %q, want the plugin name embedded in the stub", buf.String())
	}
}

// assertValidGoSnippet wraps snippet in a minimal package declaration
// (the generated stub is a map-literal entry body meant to be pasted
// directly inside internal/redteam/redteam.go, using that package's own
// bare identifiers like GradingDeterministic — not a complete,
// standalone file) and parses it with go/parser to prove it's
// syntactically valid Go. go/parser only checks syntax, not symbol
// resolution — it doesn't matter that GradingDeterministic etc. aren't
// actually declared in this wrapper; that's exactly why no import is
// needed here.
func assertValidGoSnippet(t *testing.T, snippet string) {
	t.Helper()
	wrapped := "package redteam\n\nvar _ = map[string]Plugin{\n" + snippet + "\n}\n"
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "stub.go", wrapped, parser.AllErrors); err != nil {
		t.Errorf("generated stub is not valid Go: %v\n--- snippet ---\n%s", err, snippet)
	}
}
