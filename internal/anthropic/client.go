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
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Send issues one Messages API call and returns the concatenated text content
// plus the input token count the API reports (used for max_tokens_loaded assertions).
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, err
	}

	var parsed sendResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Message{}, fmt.Errorf("decoding response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := "unknown error"
		if parsed.Error != nil {
			msg = parsed.Error.Message
		}
		return Message{}, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, msg)
	}

	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return Message{Text: text, InputTokens: parsed.Usage.InputTokens}, nil
}
