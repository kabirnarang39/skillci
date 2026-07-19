package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hello from claude"}},
			"usage":   map[string]int{"input_tokens": 42},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	msg, err := c.Send(context.Background(), "claude-sonnet-5", "You are a test skill.", "hi")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if msg.Text != "hello from claude" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello from claude")
	}
	if msg.InputTokens != 42 {
		t.Errorf("InputTokens = %d, want 42", msg.InputTokens)
	}
}

func TestSendErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	_, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err == nil {
		t.Error("Send() error = nil, want error on 429")
	}
}
