package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestSendParsesOutputTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hello"}},
			"usage":   map[string]int{"input_tokens": 42, "output_tokens": 17},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	msg, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if msg.OutputTokens != 17 {
		t.Errorf("OutputTokens = %d, want 17", msg.OutputTokens)
	}
}

func TestSendMeasuresLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hello"}},
			"usage":   map[string]int{"input_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	msg, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if msg.Latency < 50*time.Millisecond {
		t.Errorf("Latency = %v, want at least 50ms (the stub server's deliberate delay)", msg.Latency)
	}
}
