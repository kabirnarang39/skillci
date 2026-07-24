// Package githubpr opens a pull request via the GitHub REST API.
package githubpr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Open creates a pull request from head into base and returns its HTML
// URL. apiBaseURL is passed explicitly (production always uses
// "https://api.github.com") so tests can point it at a local stub server
// instead — the same pattern internal/upload uses for the dashboard URL.
func Open(ctx context.Context, apiBaseURL, token, owner, repo, head, base, title, body string) (string, error) {
	payload, err := json.Marshal(struct {
		Title string `json:"title"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Body  string `json:"body"`
	}{Title: title, Head: head, Base: base, Body: body})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls", apiBaseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github pull request creation failed with status %d: %s", resp.StatusCode, respBody)
	}

	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.HTMLURL, nil
}
