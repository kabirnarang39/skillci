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

func TestFuzzCmdSkipsCasesWithoutFuzzAssertion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true\nhi"}},
			"usage":   map[string]int{"input_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo.\n---\nBody.\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
	os.WriteFile(filepath.Join(dir, "evals", "plain.yaml"), []byte("name: plain\nprompt: hi\nassert:\n  triggered: true\n"), 0o644)

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newFuzzCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want empty — no cases have fuzz: true", out.String())
	}
}

func TestFuzzCmdRunsFuzzEnabledCases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "SKILLCI_TRIGGERED: true\nhi"}},
			"usage":   map[string]int{"input_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo.\n---\nBody.\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
	os.WriteFile(filepath.Join(dir, "evals", "fuzzy.yaml"), []byte("name: fuzzy\nprompt: \"Can you write a haiku?\"\nassert:\n  triggered: true\n  fuzz: true\n"), 0o644)

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newFuzzCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("[FUZZ]")) {
		t.Errorf("output = %q, want a [FUZZ] summary line", out.String())
	}
}

func TestFuzzCmdRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	cmd := newFuzzCmd()
	cmd.SetArgs([]string{t.TempDir()})
	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want an error when ANTHROPIC_API_KEY is unset")
	}
}
