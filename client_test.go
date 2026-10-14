package systemone

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var sentimentCriteria = map[string]any{
	"positive": "Customer is happy overall",
	"negative": "Customer is unhappy overall",
	"mixed":    "Customer has both good and bad things to say",
}

// docRequest returns the multi-question example from the DigitalOcean System One API docs.
func docRequest() *Request {
	return &Request{
		Model: "typesafe-jev-1.13.0",
		State: "The customer said the delivery was late but the product quality was excellent.",
		Questions: map[string]Question{
			"sentiment":      Choice("What is the overall sentiment of this feedback?", sentimentCriteria),
			"needs_followup": Noul("Does this feedback require a follow-up from the support team?"),
		},
	}
}

func decodeJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Errorf("unmarshal %q: %v", s, err)
	}
	return v
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQuestionJSON(t *testing.T) {
	for _, test := range []struct {
		name     string
		question Question
		want     string
	}{
		{"noul omits criteria", Noul("q"), `{"type":"noul","instructions":"q"}`},
		{"choice keeps nil description as null", Choice("q", map[string]any{"a": "desc", "b": nil}), `{"type":"choice","instructions":"q","criteria":{"a":"desc","b":null}}`},
		{"score lists levels in order", Score("q", "low", "medium", "high"), `{"type":"score","instructions":"q","criteria":["low","medium","high"]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.question)
			if err != nil || !reflect.DeepEqual(decodeJSON(t, string(data)), decodeJSON(t, test.want)) {
				t.Fatalf("Marshal() = %s, %v; want %s", data, err, test.want)
			}
		})
	}
}

func TestEvaluateWire(t *testing.T) {
	const docBody = `{"model": "typesafe-jev-1.13.0",
		"state": "The customer said the delivery was late but the product quality was excellent.",
		"questions": {
			"sentiment": {"type": "choice", "instructions": "What is the overall sentiment of this feedback?", "criteria": {
				"positive": "Customer is happy overall", "negative": "Customer is unhappy overall", "mixed": "Customer has both good and bad things to say"}},
			"needs_followup": {"type": "noul", "instructions": "Does this feedback require a follow-up from the support team?"}}}`
	for _, test := range []struct {
		name, suffix, path string
		opts               []Option
		auth               []string
	}{
		{"no token sends no Authorization", "", "/v1/systemone", nil, nil},
		{"token sends Bearer", "", "/v1/systemone", []Option{WithToken("k")}, []string{"Bearer k"}},
		{"base URL trailing slash trimmed", "/", "/v1/systemone", nil, nil},
		{"base URL path prefix kept", "/prefix", "/prefix/v1/systemone", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if ct := r.Header.Get("Content-Type"); r.Method != http.MethodPost || r.URL.Path != test.path || ct != "application/json" {
					t.Errorf("request = %s %s with Content-Type %q; want POST %s with application/json", r.Method, r.URL.Path, ct, test.path)
				}
				if auth := r.Header["Authorization"]; !reflect.DeepEqual(auth, test.auth) {
					t.Errorf("Authorization = %q; want %q", auth, test.auth)
				}
				body, _ := io.ReadAll(r.Body)
				if !reflect.DeepEqual(decodeJSON(t, string(body)), decodeJSON(t, docBody)) {
					t.Errorf("body = %s; want %s", body, docBody)
				}
				io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			if _, err := New(append([]Option{WithBaseURL(srv.URL + test.suffix)}, test.opts...)...).Evaluate(context.Background(), docRequest()); err != nil || hits.Load() != 1 {
				t.Fatalf("Evaluate() = %v after %d requests; want success after 1", err, hits.Load())
			}
		})
	}
}

func TestNewDefaults(t *testing.T) {
	var urls []string
	fake := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		status := http.StatusOK
		if len(urls) == 1 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After-Ms": {"20"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	start := time.Now()
	_, err := New(WithHTTPClient(&http.Client{Transport: fake})).Evaluate(context.Background(), docRequest())
	if elapsed := time.Since(start); err != nil || len(urls) != 2 || urls[0] != DefaultBaseURL+"/v1/systemone" || elapsed < 20*time.Millisecond {
		t.Fatalf("Evaluate() = %v after requests to %q in %v; want success after a real 20ms wait and a retry to DefaultBaseURL", err, urls, elapsed)
	}
	if DefaultBaseURL != "https://inference.do-ai.run" {
		t.Errorf("DefaultBaseURL = %q; want https://inference.do-ai.run", DefaultBaseURL)
	}
}

func TestEvaluateDecode(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       *Response
	}{
		{"choice", `{"model":"typesafe-jev-1.13.0","answers":{"sentiment":{"type":"choice","choice":"mixed","probabilities":{"mixed":1,"negative":0,"positive":0},"confidence":1}},"usage":{"input_tokens":349,"output_tokens":39}}`, &Response{
			Model:   "typesafe-jev-1.13.0",
			Answers: map[string]Answer{"sentiment": {Type: TypeChoice, Choice: "mixed", Probabilities: map[string]float64{"mixed": 1, "negative": 0, "positive": 0}, Confidence: 1}},
			Usage:   Usage{InputTokens: 349, OutputTokens: 39},
		}},
		{"score", `{"model":"typesafe-jev-1.13.0","answers":{"satisfaction":{"type":"score","score":1.32,"legend":{"0":"low","1":"medium","2":"high"},"probabilities":{"0":0,"1":0.68,"2":0.32},"confidence":0.52}},"usage":{"input_tokens":307,"output_tokens":17}}`, &Response{
			Model:   "typesafe-jev-1.13.0",
			Answers: map[string]Answer{"satisfaction": {Type: TypeScore, Score: 1.32, Legend: map[string]any{"0": "low", "1": "medium", "2": "high"}, Probabilities: map[string]float64{"0": 0, "1": 0.68, "2": 0.32}, Confidence: 0.52}},
			Usage:   Usage{InputTokens: 307, OutputTokens: 17},
		}},
		{"noul", `{"model":"typesafe-jev-1.13.0","answers":{"needs_followup":{"type":"noul","noul":0.68}},"usage":{"input_tokens":368,"output_tokens":59}}`, &Response{
			Model:   "typesafe-jev-1.13.0",
			Answers: map[string]Answer{"needs_followup": {Type: TypeNoul, Noul: 0.68}},
			Usage:   Usage{InputTokens: 368, OutputTokens: 59},
		}},
		{"legend object levels", `{"answers":{"s":{"type":"score","legend":{"0":{"label":"low"},"1":{"label":"high"}}}}}`, &Response{
			Answers: map[string]Answer{"s": {Type: TypeScore, Legend: map[string]any{"0": map[string]any{"label": "low"}, "1": map[string]any{"label": "high"}}}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, test.body)
			}))
			defer srv.Close()
			got, err := New(WithBaseURL(srv.URL)).Evaluate(context.Background(), docRequest())
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Evaluate() = %+v, %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestEvaluateAPIError(t *testing.T) {
	for _, test := range []struct {
		name   string
		opts   []Option
		status int
		body   string
		want   APIError
	}{
		{"error envelope and 400 not retried", nil, 400, `{"error":{"code":400,"message":"model name is missing from the request","type":"invalid_request_error"}}`,
			APIError{StatusCode: 400, Type: "invalid_request_error", Message: "model name is missing from the request"}},
		{"string code ignored", nil, 400, `{"error":{"code":"invalid","message":"m","type":"t"}}`, APIError{StatusCode: 400, Type: "t", Message: "m"}},
		{"non-JSON body", []Option{WithMaxRetries(0)}, 502, "bad gateway", APIError{StatusCode: 502}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer srv.Close()
			_, err := New(append([]Option{WithBaseURL(srv.URL)}, test.opts...)...).Evaluate(context.Background(), docRequest())
			want := test.want
			want.Body = test.body
			var apiErr *APIError
			if !errors.As(err, &apiErr) || *apiErr != want {
				t.Fatalf("Evaluate() error = %#v; want %#v", err, &want)
			}
			text := cmp.Or(want.Message, want.Body)
			if msg := err.Error(); !strings.Contains(msg, strconv.Itoa(test.status)) || !strings.Contains(msg, text) {
				t.Errorf("Error() = %q; want it to contain %d and %q", msg, test.status, text)
			}
			if got := hits.Load(); got != 1 {
				t.Errorf("requests = %d; want 1", got)
			}
		})
	}
}

func TestEvaluateRetry(t *testing.T) {
	for _, test := range []struct {
		name      string
		opts      []Option
		codes     []int // one status per request, the last repeats
		header    map[string]string
		requests  int32
		errStatus int // 0 means success
		waits     []time.Duration
	}{
		{"429 with Retry-After 0", nil, []int{429, 200}, map[string]string{"Retry-After": "0"}, 2, 0, []time.Duration{0}},
		{"5xx backs off 500ms then 1s", nil, []int{503, 503, 200}, nil, 3, 0, []time.Duration{500 * time.Millisecond, time.Second}},
		{"500 is retried", nil, []int{500, 200}, nil, 2, 0, []time.Duration{500 * time.Millisecond}},
		{"default 2 retries exhausted", nil, []int{503}, nil, 3, 503, []time.Duration{500 * time.Millisecond, time.Second}},
		{"retries disabled", []Option{WithMaxRetries(0)}, []int{503}, nil, 1, 503, nil},
		{"backoff capped at 5s", []Option{WithMaxRetries(5)}, []int{503}, nil, 6, 503, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}},
		{"Retry-After seconds", nil, []int{503, 200}, map[string]string{"Retry-After": "2"}, 2, 0, []time.Duration{2 * time.Second}},
		{"retry-after-ms", nil, []int{503, 200}, map[string]string{"retry-after-ms": "250"}, 2, 0, []time.Duration{250 * time.Millisecond}},
		{"retry-after-ms beats Retry-After", nil, []int{503, 200}, map[string]string{"retry-after-ms": "250", "Retry-After": "2"}, 2, 0, []time.Duration{250 * time.Millisecond}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range test.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(test.codes[min(int(hits.Add(1)), len(test.codes))-1])
				io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			c := New(append([]Option{WithBaseURL(srv.URL)}, test.opts...)...)
			var waits []time.Duration
			c.sleep = func(_ context.Context, d time.Duration) error {
				waits = append(waits, d)
				return nil
			}
			_, err := c.Evaluate(context.Background(), docRequest())
			var apiErr *APIError
			if test.errStatus == 0 && err != nil {
				t.Errorf("Evaluate() error = %v; want success", err)
			} else if test.errStatus != 0 && (!errors.As(err, &apiErr) || apiErr.StatusCode != test.errStatus) {
				t.Errorf("Evaluate() error = %v; want *APIError with status %d", err, test.errStatus)
			}
			if got := hits.Load(); got != test.requests {
				t.Errorf("requests = %d; want %d", got, test.requests)
			}
			if !reflect.DeepEqual(waits, test.waits) {
				t.Errorf("waits = %v; want %v", waits, test.waits)
			}
		})
	}
}

func TestEvaluateRetriesTransportError(t *testing.T) {
	var hits, trips atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	flaky := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if trips.Add(1) == 1 {
			return nil, errors.New("boom")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	c := New(WithBaseURL(srv.URL), WithHTTPClient(&http.Client{Transport: flaky}))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := c.Evaluate(context.Background(), docRequest()); err != nil || hits.Load() != 1 || trips.Load() != 2 {
		t.Fatalf("Evaluate() = %v after %d requests and %d round trips; want success after 1 request and 2 round trips", err, hits.Load(), trips.Load())
	}
}

func TestEvaluateCanceledDuringWait(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(WithBaseURL(srv.URL))
	c.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := c.Evaluate(ctx, docRequest()); !errors.Is(err, context.Canceled) || hits.Load() != 1 {
		t.Fatalf("Evaluate() error = %v after %d requests; want context.Canceled after 1", err, hits.Load())
	}
}

func TestEvaluateCanceledDuringDefaultSleep(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	// No c.sleep seam: the real timer wait must return once ctx is canceled.
	_, err := New(WithBaseURL(srv.URL)).Evaluate(ctx, docRequest())
	if elapsed := time.Since(start); !errors.Is(err, context.Canceled) || hits.Load() != 1 || elapsed >= 2*time.Second {
		t.Fatalf("Evaluate() error = %v after %d requests in %v; want context.Canceled after 1 within 2s", err, hits.Load(), elapsed)
	}
}
