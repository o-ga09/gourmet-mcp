// Package jev は TypeSafe AI の System One モデル Jev を呼び出すクライアント。
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBaseURL = "https://api.typesafe.ai"
	defaultModel   = "jev-latest"
	questionKey    = "match"

	// statusOverloaded は Jev が過負荷時に返す独自ステータス。
	statusOverloaded = 529
)

// NoulQuestion は真偽を 0〜1 で判定させる質問。
type NoulQuestion struct {
	Instructions string
	True         string
	False        string
}

type Client struct {
	apiKey      string
	baseURL     string
	model       string
	httpClient  *http.Client
	maxAttempts int
	baseBackoff time.Duration
}

type Option func(*Client)

func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

func WithMaxAttempts(n int) Option { return func(c *Client) { c.maxAttempts = n } }

func WithBaseBackoff(d time.Duration) Option { return func(c *Client) { c.baseBackoff = d } }

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:      apiKey,
		baseURL:     defaultBaseURL,
		model:       defaultModel,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		maxAttempts: 4,
		baseBackoff: 500 * time.Millisecond,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type requestBody struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]question `json:"questions"`
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

type responseBody struct {
	Answers map[string]struct {
		Noul *float64 `json:"noul"`
	} `json:"answers"`
}

// Noul は state に対して q を判定し、「真」である確率（0〜1）を返す。
// 429 / 529 は exponential backoff でリトライする。
func (c *Client) Noul(ctx context.Context, state any, q NoulQuestion) (float64, error) {
	reqBody := requestBody{
		Model: c.model,
		State: state,
		Questions: map[string]question{
			questionKey: {
				Type:         "noul",
				Instructions: q.Instructions,
				Criteria:     map[string]string{"true": q.True, "false": q.False},
			},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshal jev request: %w", err)
	}

	var lastErr error
	for attempt := range c.maxAttempts {
		if attempt > 0 {
			if err := sleep(ctx, c.baseBackoff<<(attempt-1)); err != nil {
				return 0, err
			}
		}
		score, retryable, err := c.do(ctx, payload)
		if err == nil {
			return score, nil
		}
		if !retryable {
			return 0, err
		}
		lastErr = err
	}
	return 0, fmt.Errorf("jev: retry limit exceeded: %w", lastErr)
}

func (c *Client) do(ctx context.Context, payload []byte) (score float64, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(payload))
	if err != nil {
		return 0, false, fmt.Errorf("build jev request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, ctx.Err() == nil, fmt.Errorf("call jev: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == statusOverloaded
		return 0, retryable, fmt.Errorf("jev: status %d: %s", resp.StatusCode, body)
	}

	var rb responseBody
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		return 0, false, fmt.Errorf("decode jev response: %w", err)
	}
	ans, ok := rb.Answers[questionKey]
	if !ok || ans.Noul == nil {
		return 0, false, fmt.Errorf("jev: answer %q not found in response", questionKey)
	}
	return *ans.Noul, false, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
