package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Result mirrors dashboard.IngestPayload's fields — kept as a separate type
// (not an import of internal/dashboard) so the CLI binary never pulls in
// the Postgres driver. JSON tags must stay in sync with Task 16's
// dashboard.IngestPayload by hand; a mismatch here would silently drop
// fields server-side rather than fail to compile, so the field-name
// alignment is enforced by TestSendPostsExpectedPayloadAndAuth above.
type DimensionEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Passed bool   `json:"passed"`
}

type Result struct {
	RepoOwner  string           `json:"repo_owner"`
	Repo       string           `json:"repo"`
	Skill      string           `json:"skill_name"`
	CommitSHA  string           `json:"commit_sha"`
	Model      string           `json:"model"`
	Passed     bool             `json:"pass"`
	Dimensions []DimensionEntry `json:"dimensions,omitempty"`
}

func Send(ctx context.Context, dashboardURL, token string, r Result) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashboardURL+"/api/v1/results", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("dashboard upload failed with status %d", resp.StatusCode)
	}
	return nil
}
