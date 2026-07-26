package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPToolsRegistersExpectedSet(t *testing.T) {
	tools := mcpTools()
	want := map[string]bool{
		"init": true, "check": true, "eval": true, "regress": true,
		"fuzz": true, "bisect": true, "accept": true, "diff": true,
		"badge": true, "report": true,
	}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(tools), len(want))
	}
	for _, tool := range tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool %q registered", tool.Name)
		}
		delete(want, tool.Name)
		if tool.Handler == nil {
			t.Errorf("tool %q has a nil Handler", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has an empty Description", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("tool %q InputSchema.type = %v, want object", tool.Name, tool.InputSchema["type"])
		}
	}
	if len(want) != 0 {
		t.Errorf("missing tool(s): %v", want)
	}
}

// TestMCPToolsRequirePathArgument covers every tool whose schema marks
// "path" required: calling it with an empty/missing path must be a
// protocol-level error (non-nil error return), not silently defaulting to
// "." the way the underlying CLI command itself would — an MCP tool call
// has no meaningful "current directory," so ambiguity here must fail
// loudly instead of operating on a directory nobody specified.
func TestMCPToolsRequirePathArgument(t *testing.T) {
	toolsRequiringPath := []string{"init", "check", "eval", "regress", "fuzz", "badge", "report"}
	byName := map[string]func(json.RawMessage) (string, bool, error){}
	for _, tool := range mcpTools() {
		byName[tool.Name] = tool.Handler
	}

	for _, name := range toolsRequiringPath {
		handler, ok := byName[name]
		if !ok {
			t.Fatalf("tool %q not found in registry", name)
		}
		args := map[string]interface{}{}
		if name == "report" {
			args["compliance"] = "nist-ai-rmf" // satisfy report's other required field so only path's absence is under test
		}
		raw, _ := json.Marshal(args)
		_, _, err := handler(raw)
		if err == nil {
			t.Errorf("tool %q: Handler err = nil with no path argument, want a protocol-level error", name)
		}
	}
}

// TestMCPToolsRequireCaseNameArgument covers the case-name-keyed tools
// (bisect, accept, diff) the same way.
func TestMCPToolsRequireCaseNameArgument(t *testing.T) {
	byName := map[string]func(json.RawMessage) (string, bool, error){}
	for _, tool := range mcpTools() {
		byName[tool.Name] = tool.Handler
	}

	for _, name := range []string{"bisect", "accept", "diff"} {
		handler := byName[name]
		raw, _ := json.Marshal(map[string]interface{}{"path": "/tmp/somewhere"})
		_, _, err := handler(raw)
		if err == nil {
			t.Errorf("tool %q: Handler err = nil with no case_name argument, want a protocol-level error", name)
		}
	}
}

// TestMCPToolCheckRunsRealCommandEndToEnd is the reachability test proving
// the "check" tool's Handler actually invokes the real cobra check
// command (via runCobraCommand) against a real skill directory, rather
// than being wired to a no-op — the same guarantee that lets this
// registry never drift from the CLI's own behavior.
func TestMCPToolCheckRunsRealCommandEndToEnd(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n" // missing description -> one real lint issue
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var checkHandler func(json.RawMessage) (string, bool, error)
	for _, tool := range mcpTools() {
		if tool.Name == "check" {
			checkHandler = tool.Handler
		}
	}

	raw, _ := json.Marshal(map[string]interface{}{"path": dir})
	text, isErr, err := checkHandler(raw)
	if err != nil {
		t.Fatalf("Handler err = %v, want nil", err)
	}
	if !isErr {
		t.Error("isError = false, want true — the skill has a real missing-description issue")
	}
	if !strings.Contains(text, "missing-description") {
		t.Errorf("text = %q, want it to contain the real missing-description finding", text)
	}
}

// TestMCPToolCheckModeWarnMatchesCLIFlag proves the "mode" argument
// actually reaches the real --mode flag, not just that the tool runs.
func TestMCPToolCheckModeWarnMatchesCLIFlag(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var checkHandler func(json.RawMessage) (string, bool, error)
	for _, tool := range mcpTools() {
		if tool.Name == "check" {
			checkHandler = tool.Handler
		}
	}

	raw, _ := json.Marshal(map[string]interface{}{"path": dir, "mode": "warn"})
	_, isErr, err := checkHandler(raw)
	if err != nil {
		t.Fatalf("Handler err = %v, want nil", err)
	}
	if isErr {
		t.Error("isError = true, want false — mode: warn must never fail the tool call")
	}
}

// TestMCPToolReportRunsRealCommandEndToEnd proves the "report" tool
// reaches the real report command with both required arguments correctly
// mapped (path positional, compliance flag).
func TestMCPToolReportRunsRealCommandEndToEnd(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: my-skill\ndescription: Does a thing.\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var reportHandler func(json.RawMessage) (string, bool, error)
	for _, tool := range mcpTools() {
		if tool.Name == "report" {
			reportHandler = tool.Handler
		}
	}

	raw, _ := json.Marshal(map[string]interface{}{"path": dir, "compliance": "eu-ai-act"})
	text, isErr, err := reportHandler(raw)
	if err != nil {
		t.Fatalf("Handler err = %v, want nil", err)
	}
	if isErr {
		t.Errorf("isError = true, want false for a successful report generation")
	}
	if !strings.Contains(text, "EU AI Act") {
		t.Errorf("text = %q, want the real EU AI Act report content", text)
	}
}
