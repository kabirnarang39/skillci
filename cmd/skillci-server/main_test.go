package main

import (
	"testing"

	"github.com/kabirnarang39/skillci/internal/dashboard"
)

func TestParseTokensLegacySingleUnscopedToken(t *testing.T) {
	t.Setenv("SKILLCI_INGEST_TOKENS", "")
	t.Setenv("SKILLCI_INGEST_TOKEN", "legacy-token")

	tokens, err := parseTokens()
	if err != nil {
		t.Fatalf("parseTokens() error = %v", err)
	}
	if len(tokens) != 1 || tokens[0] != (dashboard.TokenScope{Token: "legacy-token"}) {
		t.Errorf("tokens = %+v, want a single unscoped legacy-token entry", tokens)
	}
}

func TestParseTokensScopedMultipleEntries(t *testing.T) {
	t.Setenv("SKILLCI_INGEST_TOKENS", "token-a=org-a/repo-a,token-b=org-b/repo-b")
	t.Setenv("SKILLCI_INGEST_TOKEN", "")

	tokens, err := parseTokens()
	if err != nil {
		t.Fatalf("parseTokens() error = %v", err)
	}
	want := []dashboard.TokenScope{
		{Token: "token-a", Owner: "org-a", Repo: "repo-a"},
		{Token: "token-b", Owner: "org-b", Repo: "repo-b"},
	}
	if len(tokens) != len(want) {
		t.Fatalf("tokens = %+v, want %+v", tokens, want)
	}
	for i := range want {
		if tokens[i] != want[i] {
			t.Errorf("tokens[%d] = %+v, want %+v", i, tokens[i], want[i])
		}
	}
}

func TestParseTokensScopedTakesPrecedenceOverLegacy(t *testing.T) {
	t.Setenv("SKILLCI_INGEST_TOKENS", "token-a=org-a/repo-a")
	t.Setenv("SKILLCI_INGEST_TOKEN", "legacy-token")

	tokens, err := parseTokens()
	if err != nil {
		t.Fatalf("parseTokens() error = %v", err)
	}
	if len(tokens) != 1 || tokens[0].Token != "token-a" {
		t.Errorf("tokens = %+v, want SKILLCI_INGEST_TOKENS to take precedence over the legacy var", tokens)
	}
}

func TestParseTokensRejectsMalformedEntry(t *testing.T) {
	t.Setenv("SKILLCI_INGEST_TOKENS", "not-a-valid-entry")
	t.Setenv("SKILLCI_INGEST_TOKEN", "")

	if _, err := parseTokens(); err == nil {
		t.Fatal("parseTokens() error = nil, want an error for an entry missing the = separator")
	}
}

func TestParseTokensRejectsMissingOwnerOrRepo(t *testing.T) {
	t.Setenv("SKILLCI_INGEST_TOKENS", "token-a=justonesegment")
	t.Setenv("SKILLCI_INGEST_TOKEN", "")

	if _, err := parseTokens(); err == nil {
		t.Fatal("parseTokens() error = nil, want an error for an owner/repo missing the / separator")
	}
}

func TestParseTokensErrorsWhenNeitherVarIsSet(t *testing.T) {
	t.Setenv("SKILLCI_INGEST_TOKENS", "")
	t.Setenv("SKILLCI_INGEST_TOKEN", "")

	if _, err := parseTokens(); err == nil {
		t.Fatal("parseTokens() error = nil, want an error when neither env var is set")
	}
}
