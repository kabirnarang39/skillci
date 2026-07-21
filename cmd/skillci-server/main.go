package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/kabirnarang39/skillci/internal/dashboard"
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
