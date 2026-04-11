package client

import (
	"net/http"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func newMockResponse(statusCode int, headers map[string]string) *resty.Response {
	rawResp := &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{},
	}
	for k, v := range headers {
		rawResp.Header.Set(k, v)
	}
	return &resty.Response{RawResponse: rawResp}
}

func TestParseRetryAfter_EmptyHeader(t *testing.T) {
	resp := newMockResponse(429, nil)
	d, err := parseRetryAfter(nil, resp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if d != 0 {
		t.Fatalf("expected 0 duration for empty header, got %v", d)
	}
}

func TestParseRetryAfter_IntegerSeconds(t *testing.T) {
	resp := newMockResponse(429, map[string]string{"Retry-After": "120"})
	d, err := parseRetryAfter(nil, resp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Should be 120s + up to 10% jitter (0-12s), so between 120s and 132s.
	if d < 120*time.Second || d > 132*time.Second {
		t.Fatalf("expected duration in [120s, 132s], got %v", d)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	// Set a date 60 seconds in the future.
	future := time.Now().Add(60 * time.Second).UTC().Format(http.TimeFormat)
	resp := newMockResponse(429, map[string]string{"Retry-After": future})
	d, err := parseRetryAfter(nil, resp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Should be roughly 60s + jitter. Allow a wide window for clock imprecision.
	if d < 50*time.Second || d > 70*time.Second {
		t.Fatalf("expected duration around 60s, got %v", d)
	}
}

func TestParseRetryAfter_PastHTTPDate(t *testing.T) {
	past := time.Now().Add(-60 * time.Second).UTC().Format(http.TimeFormat)
	resp := newMockResponse(429, map[string]string{"Retry-After": past})
	d, err := parseRetryAfter(nil, resp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if d != 0 {
		t.Fatalf("expected 0 for a past date, got %v", d)
	}
}

func TestParseRetryAfter_GarbageValue(t *testing.T) {
	resp := newMockResponse(429, map[string]string{"Retry-After": "not-a-number-or-date"})
	d, err := parseRetryAfter(nil, resp)
	if err != nil {
		t.Fatalf("expected no error on garbage input (should fall back to backoff), got %v", err)
	}
	if d != 0 {
		t.Fatalf("expected 0 duration for garbage header, got %v", d)
	}
}
