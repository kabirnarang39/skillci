package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if !strings.Contains(out.String(), "no fuzz-enabled eval cases found in evals/") {
		t.Errorf("output = %q, want it to mention no fuzz-enabled eval cases found", out.String())
	}
}

func TestFuzzCmdRunsFuzzEnabledCases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		text := "SKILLCI_TRIGGERED: true\nhi"
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "don't") {
			// The negation mutation that inserts "don't" before the verb flips the outcome
			text = "SKILLCI_TRIGGERED: false"
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
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

func TestFuzzCmdSkipsAssertionsWhenCaseFails(t *testing.T) {
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
	// Case with fuzz: true but triggered: false — case will fail, fuzzing won't run
	os.WriteFile(filepath.Join(dir, "evals", "fuzzy_fails.yaml"), []byte("name: fuzzy_fails\nprompt: \"hi\"\nassert:\n  triggered: false\n  fuzz: true\n"), 0o644)

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newFuzzCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err == nil {
		t.Error("Execute() error = nil, want an error when case assertion fails")
	}
	if bytes.Contains(out.Bytes(), []byte("[FUZZ]")) {
		t.Errorf("output = %q, [FUZZ] should not appear when fuzzing didn't run", out.String())
	}
}

func TestFuzzCmdPrintsLatencyWarningWhenExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
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
	caseContent := "name: fuzzy-latency\nprompt: \"Can you write a haiku?\"\nassert:\n  triggered: true\n  fuzz: true\n  max_latency_ms: 1\n"
	os.WriteFile(filepath.Join(dir, "evals", "fuzzy_latency.yaml"), []byte(caseContent), 0o644)

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("SKILLCI_BASE_URL", srv.URL)

	cmd := newFuzzCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v; output = %s", err, out.String())
	}

	if !strings.Contains(out.String(), "[LATENCY]") {
		t.Errorf("output = %q, want a [LATENCY] line", out.String())
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
