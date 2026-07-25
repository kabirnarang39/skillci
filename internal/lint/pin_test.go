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

// TestIsPublicPinnedSourceIP directly exercises the SSRF guard's actual
// decision function — previously only ever hit indirectly, through one
// integration test blocking a single loopback address. Covers every
// non-public category the guard is supposed to block (not just loopback),
// including 169.254.169.254 specifically: the real-world AWS/GCP/Azure
// cloud metadata endpoint this guard exists to protect against, and a
// case proving a genuine public address is allowed through — the "happy
// path" no other test exercises at all, since every existing test either
// hits a blocked address or swaps the dialer out entirely.
func TestIsPublicPinnedSourceIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"public IPv4", "93.184.216.34", true},
		{"public IPv6", "2606:2800:220:1:248:1893:25c8:1946", true},
		{"loopback IPv4", "127.0.0.1", false},
		{"loopback IPv6", "::1", false},
		{"private IPv4 class A", "10.0.0.1", false},
		{"private IPv4 class B", "172.16.0.1", false},
		{"private IPv4 class C", "192.168.1.1", false},
		{"private IPv6 ULA", "fc00::1", false},
		{"link-local IPv4 (cloud metadata endpoint)", "169.254.169.254", false},
		{"link-local IPv6", "fe80::1", false},
		{"unspecified IPv4", "0.0.0.0", false},
		{"unspecified IPv6", "::", false},
		{"multicast IPv4", "224.0.0.1", false},
		{"multicast IPv6", "ff02::1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) = nil", tt.ip)
			}
			if got := isPublicPinnedSourceIP(ip); got != tt.want {
				t.Errorf("isPublicPinnedSourceIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// TestSafeDialContextRejectsUnresolvableHost covers a pinned_sources
// hostname that doesn't resolve at all (typo, deleted DNS record) —
// previously untested; every existing unreachable-source test used a raw
// IP literal, which skips DNS resolution entirely and so never exercised
// this error path.
func TestSafeDialContextRejectsUnresolvableHost(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "this-host-does-not-resolve.invalid:80")
	if err == nil {
		t.Fatal("safeDialContext() error = nil, want an error for an unresolvable host")
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
