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
