package systemone

import (
	"context"
	"errors"
	"maps"
	"math"
	"os"
	"slices"
	"testing"
	"time"
)

func TestE2EEvaluate(t *testing.T) {
	base, model := os.Getenv("SYSTEMONE_BASE_URL"), os.Getenv("SYSTEMONE_MODEL")
	if base == "" || model == "" {
		t.Skip("set SYSTEMONE_BASE_URL and SYSTEMONE_MODEL (optionally SYSTEMONE_TOKEN) to call a live endpoint")
	}
	c := New(WithBaseURL(base), WithToken(os.Getenv("SYSTEMONE_TOKEN")))
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	req := docRequest()
	req.Model = model
	req.Questions["satisfaction"] = Score("Rate overall customer satisfaction from low to high.", "low", "medium", "high")
	resp, err := c.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	t.Logf("Evaluate() = %+v", resp)
	choice, score, noul := resp.Answers["sentiment"], resp.Answers["satisfaction"], resp.Answers["needs_followup"]
	if choice.Type != TypeChoice || score.Type != TypeScore || noul.Type != TypeNoul {
		t.Errorf("answer types = %q, %q, %q; want %q, %q, %q", choice.Type, score.Type, noul.Type, TypeChoice, TypeScore, TypeNoul)
	}
	sum := 0.0
	for _, p := range choice.Probabilities {
		sum += p
	}
	if _, ok := sentimentCriteria[choice.Choice]; !ok || math.Abs(sum-1) > 0.01 || choice.Confidence < 0 || choice.Confidence > 1 ||
		!slices.Equal(slices.Sorted(maps.Keys(choice.Probabilities)), slices.Sorted(maps.Keys(sentimentCriteria))) {
		t.Errorf("choice answer = %+v; want a criteria key, probabilities over exactly the criteria keys summing to 1, confidence in [0,1]", choice)
	}
	if len(score.Legend) != 3 || !slices.Equal(slices.Sorted(maps.Keys(score.Probabilities)), []string{"0", "1", "2"}) || score.Score < 0 || score.Score > 2 {
		t.Errorf("score answer = %+v; want 3 legend entries, probabilities for levels 0-2, score in [0,2]", score)
	}
	if noul.Noul < 0 || noul.Noul > 1 || resp.Usage.InputTokens <= 0 {
		t.Errorf("noul answer = %+v, usage = %+v; want noul in [0,1] and input tokens > 0", noul, resp.Usage)
	}
	req.Questions = map[string]Question{"q": {Type: "foo", Instructions: "x"}}
	_, err = c.Evaluate(ctx, req)
	t.Logf("Evaluate(unknown question type) error = %v", err)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode < 400 || apiErr.StatusCode > 499 || apiErr.Message == "" {
		t.Errorf("Evaluate(unknown question type) error = %#v; want a 4xx *APIError with a message", err)
	}
}
