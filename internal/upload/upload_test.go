package upload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPostsExpectedPayloadAndAuth(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "secret-token", Result{
		RepoOwner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", gotAuth)
	}
	if gotBody["repo_owner"] != "kabirnarang" || gotBody["skill_name"] != "pr-review" {
		t.Errorf("body = %v, field names must match dashboard.IngestPayload JSON tags", gotBody)
	}
}

func TestSendReturnsErrorOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "secret-token", Result{})
	if err == nil {
		t.Error("Send() error = nil, want error on 500")
	}
}

func TestSendPostsDimensionEntries(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "secret-token", Result{
		RepoOwner: "kabirnarang", Repo: "skillci", Skill: "pr-review",
		CommitSHA: "abc123", Model: "claude-sonnet-5", Passed: true,
		Dimensions: []DimensionEntry{{Key: "segment", Value: "enterprise", Passed: true}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	dims, ok := gotBody["dimensions"].([]any)
	if !ok || len(dims) != 1 {
		t.Fatalf("body[dimensions] = %v, want a 1-element array", gotBody["dimensions"])
	}
	entry := dims[0].(map[string]any)
	if entry["key"] != "segment" || entry["value"] != "enterprise" {
		t.Errorf("dimension entry = %v, want key=segment value=enterprise", entry)
	}
}
