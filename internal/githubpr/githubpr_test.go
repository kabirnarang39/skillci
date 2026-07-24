package githubpr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenPostsExpectedPayloadAndAuthAndReturnsURL(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotAccept string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("request body did not decode as JSON: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"html_url": "https://github.com/acme/widget/pull/42"})
	}))
	defer srv.Close()

	url, err := Open(context.Background(), srv.URL, "test-token", "acme", "widget", "skillci/generated-eval-1", "main", "add generated case", "opened automatically")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if url != "https://github.com/acme/widget/pull/42" {
		t.Errorf("url = %q, want %q", url, "https://github.com/acme/widget/pull/42")
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/repos/acme/widget/pulls" {
		t.Errorf("path = %q, want /repos/acme/widget/pulls", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
	if !strings.Contains(gotAccept, "application/vnd.github+json") {
		t.Errorf("Accept = %q, want it to request the GitHub JSON media type", gotAccept)
	}
	if gotBody["head"] != "skillci/generated-eval-1" || gotBody["base"] != "main" || gotBody["title"] != "add generated case" || gotBody["body"] != "opened automatically" {
		t.Errorf("request body = %+v, want head/base/title/body to match the call's arguments", gotBody)
	}
}

func TestOpenReturnsErrorOnNonCreatedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message": "A pull request already exists"}`))
	}))
	defer srv.Close()

	_, err := Open(context.Background(), srv.URL, "test-token", "acme", "widget", "skillci/generated-eval-1", "main", "title", "body")
	if err == nil {
		t.Fatal("Open() error = nil, want an error on a non-201 response")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error = %v, want it to mention the status code", err)
	}
}
