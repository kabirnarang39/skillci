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
	defer stubZeroBackoff()()

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

// stubZeroBackoff swaps sendBackoff for a zero-delay stub so retry tests
// don't spend real wall-clock time on exponential backoff, and returns a
// restore func to defer.
func stubZeroBackoff() func() {
	orig := sendBackoff
	sendBackoff = func(attempt int) time.Duration { return 0 }
	return func() { sendBackoff = orig }
}

// TestSendRetriesOnRetryableStatusThenSucceeds covers a transient
// 503 ("service unavailable") followed by a real success — a single flaky
// response used to fail the entire call (and, upstream, an entire regress
// run's worth of already-completed API calls, since RunMatrix aborts the
// whole matrix on the first error) even though a retry would have
// succeeded seconds later.
func TestSendRetriesOnRetryableStatusThenSucceeds(t *testing.T) {
	defer stubZeroBackoff()()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"overloaded"}}`))
			return
		}
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "recovered"}},
			"usage":   map[string]int{"input_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	msg, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err != nil {
		t.Fatalf("Send() error = %v, want success after retrying past transient 503s", err)
	}
	if msg.Text != "recovered" {
		t.Errorf("Text = %q, want %q", msg.Text, "recovered")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3 (2 failures + 1 success)", attempts)
	}
}

// TestSendDoesNotRetryNonRetryableStatus covers a genuine client error
// (401, bad API key) — retrying can never succeed, so it must fail on the
// first attempt instead of wasting time and quota on retries that are
// guaranteed to fail identically.
func TestSendDoesNotRetryNonRetryableStatus(t *testing.T) {
	defer stubZeroBackoff()()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	_, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err == nil {
		t.Fatal("Send() error = nil, want error on 401")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 — a 401 must never be retried", attempts)
	}
}

// TestSendGivesUpAfterMaxAttempts covers a server that never recovers:
// retries must be bounded, not infinite.
func TestSendGivesUpAfterMaxAttempts(t *testing.T) {
	defer stubZeroBackoff()()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer srv.Close()

	c := NewClient("test-key").WithBaseURL(srv.URL)
	_, err := c.Send(context.Background(), "claude-sonnet-5", "sys", "hi")
	if err == nil {
		t.Fatal("Send() error = nil, want error after exhausting retries against a server that never recovers")
	}
	if attempts != maxSendAttempts {
		t.Errorf("attempts = %d, want exactly maxSendAttempts (%d)", attempts, maxSendAttempts)
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
