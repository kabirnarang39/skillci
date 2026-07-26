package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.anthropic.com"
const apiVersion = "2023-06-01"

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) WithBaseURL(url string) *Client {
	c.baseURL = url
	return c
}

type Message struct {
	Text        string
	InputTokens int
	// OutputTokens and Latency are always populated, independent of
	// whether any eval-case assertion uses them — see Result.OutputTokens
	// and Result.LatencyMs in internal/runner.
	OutputTokens int
	Latency      time.Duration
}

type sendRequest struct {
	Model     string   `json:"model"`
	MaxTokens int      `json:"max_tokens"`
	System    string   `json:"system,omitempty"`
	Messages  []reqMsg `json:"messages"`
}

type reqMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sendResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// retryableStatuses are worth retrying: rate limits and transient
// server-side failures. Anything else (400 bad request, 401 invalid key,
// 404, ...) will fail identically on every retry, so retrying would only
// waste time and API quota.
var retryableStatuses = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:          true, // 502
	http.StatusServiceUnavailable:  true, // 503
	529:                            true, // Anthropic-specific "overloaded"
}

// maxSendAttempts bounds retries: 1 initial attempt + 3 retries. A single
// case/model call is one of potentially hundreds in a regress run — since
// RunMatrix aborts the whole matrix on the first error, an unretried
// transient failure used to waste every already-completed call in that
// run, not just the one that hit it. Bounded rather than unlimited so a
// server that never recovers still fails in bounded time instead of
// retrying forever.
const maxSendAttempts = 4

// sendBackoff is the delay before retry attempt n (n=1 is the delay before
// the 2nd overall attempt): 500ms, 1s, 2s. A package var so tests can
// stub in a zero-delay function instead of spending real wall-clock time
// on exponential backoff.
var sendBackoff = func(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
}

// Send issues one Messages API call and returns the concatenated text content
// plus the input token count the API reports (used for max_tokens_loaded assertions).
// Retries a bounded number of times on a retryable transient failure
// (network error or a retryableStatuses HTTP status); Latency measures only
// the final, successful attempt, not time spent on earlier failures or
// backoff, so it stays a meaningful signal for max_latency_ms assertions
// instead of being inflated by unrelated network flakiness.
func (c *Client) Send(ctx context.Context, model, systemPrompt, userPrompt string) (Message, error) {
	body, err := json.Marshal(sendRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  []reqMsg{{Role: "user", Content: userPrompt}},
	})
	if err != nil {
		return Message{}, err
	}
	return c.sendWithRetry(ctx, body)
}

// sendWithRetry issues one Messages API call for the given pre-marshaled
// request body, retrying a bounded number of times on a retryable
// transient failure. Shared by Send (single-turn) and SendConversation
// (full message history) — retry/backoff behavior is identical either
// way, only how body gets built differs.
func (c *Client) sendWithRetry(ctx context.Context, body []byte) (Message, error) {
	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(sendBackoff(attempt - 1)):
			case <-ctx.Done():
				return Message{}, ctx.Err()
			}
		}

		msg, retryable, err := c.sendOnce(ctx, body)
		if err == nil {
			return msg, nil
		}
		lastErr = err
		if !retryable {
			return Message{}, err
		}
	}
	return Message{}, fmt.Errorf("after %d attempts: %w", maxSendAttempts, lastErr)
}

// ConversationTurn is one message in a multi-turn conversation sent via
// SendConversation — Role is "user" or "assistant".
type ConversationTurn struct {
	Role    string
	Content string
}

// SendConversation issues one Messages API call carrying the full turns
// history, unlike Send (which always sends a single "user" message with
// no prior context) — used by redteam plugins that need multi-turn
// escalation state threaded across calls.
func (c *Client) SendConversation(ctx context.Context, model, systemPrompt string, turns []ConversationTurn) (Message, error) {
	msgs := make([]reqMsg, len(turns))
	for i, t := range turns {
		msgs[i] = reqMsg(t)
	}
	body, err := json.Marshal(sendRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  msgs,
	})
	if err != nil {
		return Message{}, err
	}
	return c.sendWithRetry(ctx, body)
}

// sendOnce issues a single Messages API call. retryable reports whether a
// non-nil err is worth retrying (a network-level failure or a
// retryableStatuses HTTP status) as opposed to a permanent failure.
func (c *Client) sendOnce(ctx context.Context, body []byte) (msg Message, retryable bool, err error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Message{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, true, err
	}
	defer resp.Body.Close() //nolint:errcheck // closing a response body we only read from; nothing meaningful to do with a close error here

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, true, err
	}

	var parsed sendResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Message{}, false, fmt.Errorf("decoding response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := "unknown error"
		if parsed.Error != nil {
			errMsg = parsed.Error.Message
		}
		return Message{}, retryableStatuses[resp.StatusCode], fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, errMsg)
	}

	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return Message{
		Text:         text,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		Latency:      time.Since(start),
	}, false, nil
}
