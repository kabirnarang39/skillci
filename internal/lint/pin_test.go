package lint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowLoopbackDial swaps out the SSRF-guarded dialer for a plain one, for
// the duration of the calling test, so it can talk to an httptest server on
// 127.0.0.1. The production default (PinnedSourceDialer = safeDialContext)
// is restored automatically via t.Cleanup.
func allowLoopbackDial(t *testing.T) {
	t.Helper()
	orig := PinnedSourceDialer
	PinnedSourceDialer = (&net.Dialer{}).DialContext
	t.Cleanup(func() { PinnedSourceDialer = orig })
}

func writePinnedSkill(t *testing.T, dir, pinnedYAML string) string {
	t.Helper()
	content := "---\nname: my-skill\ndescription: Does a thing.\n" + pinnedYAML + "---\nBody.\n"
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return skillPath
}

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestVerifyPinnedSourcesNoIssueWhenHashMatches(t *testing.T) {
	allowLoopbackDial(t)
	const content = "the exact content this skill's author reviewed and pinned"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, fmt.Sprintf("pinned_sources:\n  - url: %s\n    sha256: %s\n", srv.URL, hashOf(content)))

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("issues = %+v, want none — hash matches", issues)
	}
}

func TestVerifyPinnedSourcesFlagsHashMismatch(t *testing.T) {
	allowLoopbackDial(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "content has changed since it was pinned")
	}))
	defer srv.Close()

	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, fmt.Sprintf("pinned_sources:\n  - url: %s\n    sha256: %s\n", srv.URL, hashOf("the original pinned content")))

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "ast02-pinned-source-mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want an ast02-pinned-source-mismatch issue", issues)
	}
}

func TestVerifyPinnedSourcesFlagsUnreachableSource(t *testing.T) {
	dir := t.TempDir()
	// Port 1 is reserved/unassigned. 127.0.0.1 is also loopback, so the
	// SSRF guard below rejects it before a connection is even attempted —
	// either way, this must surface as unreachable.
	skillPath := writePinnedSkill(t, dir, "pinned_sources:\n  - url: http://127.0.0.1:1\n    sha256: "+hashOf("x")+"\n")

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "ast02-pinned-source-unreachable" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want an ast02-pinned-source-unreachable issue", issues)
	}
}

// TestVerifyPinnedSourcesRefusesNonPublicAddress proves the SSRF guard
// itself fires — a malicious skill can't point pinned_sources at a
// loopback/private/link-local address (e.g. a cloud metadata endpoint) to
// turn --verify-pinned-sources into an internal-network probe.
func TestVerifyPinnedSourcesRefusesNonPublicAddress(t *testing.T) {
	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, "pinned_sources:\n  - url: http://127.0.0.1:1/\n    sha256: "+hashOf("x")+"\n")

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "ast02-pinned-source-unreachable" && strings.Contains(iss.Msg, "non-public") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want an ast02-pinned-source-unreachable issue mentioning a non-public address", issues)
	}
}

func TestVerifyPinnedSourcesFlagsNonHTTPScheme(t *testing.T) {
	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, "pinned_sources:\n  - url: file:///etc/passwd\n    sha256: "+hashOf("x")+"\n")

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "ast02-pinned-source-invalid" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want an ast02-pinned-source-invalid issue for a non-http(s) scheme", issues)
	}
}

func TestVerifyPinnedSourcesFlagsMissingHash(t *testing.T) {
	allowLoopbackDial(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "content")
	}))
	defer srv.Close()

	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, fmt.Sprintf("pinned_sources:\n  - url: %s\n", srv.URL))

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "ast02-pinned-source-invalid" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want an ast02-pinned-source-invalid issue for a missing sha256", issues)
	}
}

func TestVerifyPinnedSourcesNoIssuesWhenNoPinnedSourcesDeclared(t *testing.T) {
	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, "")

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("issues = %+v, want none when pinned_sources isn't set at all", issues)
	}
}

func TestVerifyPinnedSourcesFlagsOversizedResponse(t *testing.T) {
	allowLoopbackDial(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, maxPinnedSourceBytes+1))
	}))
	defer srv.Close()

	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, fmt.Sprintf("pinned_sources:\n  - url: %s\n    sha256: %s\n", srv.URL, hashOf("irrelevant")))

	issues, err := VerifyPinnedSources(context.Background(), skillPath)
	if err != nil {
		t.Fatalf("VerifyPinnedSources() error = %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Rule == "ast02-pinned-source-unreachable" && strings.Contains(iss.Msg, "exceeds") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want an ast02-pinned-source-unreachable issue for an oversized response", issues)
	}
}

func TestVerifyPinnedSourcesPinnedSourcesDoesNotTriggerUnexpectedFrontmatterField(t *testing.T) {
	// pinned_sources must be in scanFrontmatterSecurity's allow-list —
	// this proves the wiring, not VerifyPinnedSources itself.
	dir := t.TempDir()
	skillPath := writePinnedSkill(t, dir, "pinned_sources:\n  - url: https://example.com/x\n    sha256: abc123\n")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	fm, _, err := splitFrontmatter(string(data))
	if err != nil {
		t.Fatal(err)
	}
	issues := scanFrontmatterSecurity(skillPath, fm)
	for _, iss := range issues {
		if iss.Rule == "ast04-unexpected-frontmatter-field" {
			t.Errorf("issues = %+v, want no ast04-unexpected-frontmatter-field issue for the documented pinned_sources field", issues)
		}
	}
}
