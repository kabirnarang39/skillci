package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/kabirnarang39/skillci/internal/dashboard"
)

// parseTokens builds the token-scope list from either env var:
//   - SKILLCI_INGEST_TOKENS (preferred for an instance shared by more than
//     one project): "token1=owner1/repo1,token2=owner2/repo2" — each token
//     only authorizes payloads claiming that exact owner/repo.
//   - SKILLCI_INGEST_TOKEN (legacy, single unscoped token) — authorizes
//     any owner/repo, matching this server's original single-project
//     self-hosted behavior. Ignored if SKILLCI_INGEST_TOKENS is set.
func parseTokens() ([]dashboard.TokenScope, error) {
	if scoped := os.Getenv("SKILLCI_INGEST_TOKENS"); scoped != "" {
		var tokens []dashboard.TokenScope
		for _, entry := range strings.Split(scoped, ",") {
			tokenAndRepo := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(tokenAndRepo) != 2 {
				return nil, fmt.Errorf("SKILLCI_INGEST_TOKENS entry %q is not in token=owner/repo form", entry)
			}
			ownerRepo := strings.SplitN(tokenAndRepo[1], "/", 2)
			if len(ownerRepo) != 2 || ownerRepo[0] == "" || ownerRepo[1] == "" {
				return nil, fmt.Errorf("SKILLCI_INGEST_TOKENS entry %q has an invalid owner/repo — want owner/repo", entry)
			}
			tokens = append(tokens, dashboard.TokenScope{Token: tokenAndRepo[0], Owner: ownerRepo[0], Repo: ownerRepo[1]})
		}
		return tokens, nil
	}
	if token := os.Getenv("SKILLCI_INGEST_TOKEN"); token != "" {
		return []dashboard.TokenScope{{Token: token}}, nil
	}
	return nil, fmt.Errorf("neither SKILLCI_INGEST_TOKENS nor SKILLCI_INGEST_TOKEN is set")
}

func main() {
	dbURL := os.Getenv("SKILLCI_DATABASE_URL")
	if dbURL == "" {
		log.Fatal("SKILLCI_DATABASE_URL is not set")
	}
	tokens, err := parseTokens()
	if err != nil {
		log.Fatal(err)
	}

	store, err := dashboard.NewStore(dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	mux := dashboard.NewServer(store, tokens)

	addr := os.Getenv("SKILLCI_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	fmt.Printf("skillci-server listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
