// Package systemone is a client for the System One API (TypeSafe Jev) on DigitalOcean Serverless Inference.
package systemone

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the DigitalOcean Serverless Inference endpoint.
const DefaultBaseURL = "https://inference.do-ai.run"

// Client sends evaluations to the System One API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	maxRetries int
	sleep      func(ctx context.Context, d time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// New returns a Client with DefaultBaseURL, http.DefaultClient, and 2 retries, adjusted by opts.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		httpClient: http.DefaultClient,
		maxRetries: 2,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
			return ctx.Err()
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithBaseURL sets the API base URL, without the /v1/systemone path.
func WithBaseURL(baseURL string) Option {
	return func(cl *Client) { cl.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithToken sets the bearer token, such as a DigitalOcean model access key.
func WithToken(token string) Option {
	return func(cl *Client) { cl.token = token }
}

// WithHTTPClient sets the HTTP client that sends requests.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// WithMaxRetries sets how many times a failed request is retried; negative values mean no retries.
func WithMaxRetries(n int) Option {
	return func(cl *Client) { cl.maxRetries = max(n, 0) }
}

// Evaluate sends req and returns its answers, retrying 429, 5xx, and transport errors; a non-2xx result is an *APIError.
func (c *Client) Evaluate(ctx context.Context, req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	for n := 0; ; n++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.httpClient.Do(httpReq)
		wait := min(500*time.Millisecond<<min(n, 4), 5*time.Second) // capping n keeps the shift from overflowing
		switch {
		case err != nil:
			if ctx.Err() != nil || n >= c.maxRetries {
				return nil, err
			}
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			defer resp.Body.Close()
			var out Response
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return nil, fmt.Errorf("systemone: decode response: %w", err)
			}
			return &out, nil
		case n >= c.maxRetries || (resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500):
			return nil, newAPIError(resp)
		default:
			if d, ok := retryAfter(resp); ok {
				wait = d
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	if ms, err := strconv.Atoi(resp.Header.Get("retry-after-ms")); err == nil {
		return time.Duration(ms) * time.Millisecond, true
	}
	v := resp.Header.Get("Retry-After")
	if s, err := strconv.Atoi(v); err == nil {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

// APIError reports a non-2xx response; Type and Message come from its JSON error envelope, if any.
type APIError struct {
	StatusCode int
	Type       string
	Message    string
	Body       string
}

// Error returns the status code with Message, or with Body when Message is empty.
func (e *APIError) Error() string {
	return fmt.Sprintf("systemone: %d: %s", e.StatusCode, cmp.Or(e.Message, e.Body))
}

func newAPIError(resp *http.Response) *APIError {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Error struct{ Message, Type string } // code is not parsed: it may be a number or a string
	}
	e := &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	if json.Unmarshal(body, &envelope) == nil {
		e.Type, e.Message = envelope.Error.Type, envelope.Error.Message
	}
	return e
}
