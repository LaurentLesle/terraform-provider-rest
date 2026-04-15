package client

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/net/http2"
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

// ── isTransientTransportError ─────────────────────────────────────────────────

func TestIsTransientTransportError_Nil(t *testing.T) {
	if isTransientTransportError(nil) {
		t.Fatal("expected false for nil error")
	}
}

func TestIsTransientTransportError_Http2InternalError_Typed(t *testing.T) {
	err := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	if !isTransientTransportError(err) {
		t.Fatal("expected true for http2.StreamError ErrCodeInternal")
	}
}

func TestIsTransientTransportError_Http2RefusedStream_Typed(t *testing.T) {
	err := http2.StreamError{StreamID: 1, Code: http2.ErrCodeRefusedStream}
	if !isTransientTransportError(err) {
		t.Fatal("expected true for http2.StreamError ErrCodeRefusedStream")
	}
}

func TestIsTransientTransportError_Http2OtherCode_Typed(t *testing.T) {
	// ErrCodeCancel is not a transient error — should not retry.
	err := http2.StreamError{StreamID: 1, Code: http2.ErrCodeCancel}
	if isTransientTransportError(err) {
		t.Fatal("expected false for http2.StreamError ErrCodeCancel")
	}
}

func TestIsTransientTransportError_Http2InternalError_Wrapped(t *testing.T) {
	// net/http often wraps the http2.StreamError; errors.As should still find it.
	inner := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	wrapped := fmt.Errorf("request failed: %w", inner)
	if !isTransientTransportError(wrapped) {
		t.Fatal("expected true for wrapped http2.StreamError ErrCodeInternal")
	}
}

func TestIsTransientTransportError_StringFallback_StreamError(t *testing.T) {
	// Simulates the exact error message from the issue report.
	err := errors.New("stream error: stream ID 1; INTERNAL_ERROR; received from peer")
	if !isTransientTransportError(err) {
		t.Fatal("expected true for 'stream error: INTERNAL_ERROR' string")
	}
}

func TestIsTransientTransportError_StringFallback_ConnectionReset(t *testing.T) {
	err := errors.New("read tcp 10.0.0.1:12345->10.0.0.2:443: connection reset by peer")
	if !isTransientTransportError(err) {
		t.Fatal("expected true for 'connection reset by peer'")
	}
}

func TestIsTransientTransportError_StringFallback_UnexpectedEOF(t *testing.T) {
	err := errors.New("unexpected EOF")
	if !isTransientTransportError(err) {
		t.Fatal("expected true for 'unexpected EOF'")
	}
}

func TestIsTransientTransportError_NotTransient_404(t *testing.T) {
	// A plain HTTP-level error (e.g. string from a 404 body) must not trigger transport retry.
	err := errors.New("404 Not Found")
	if isTransientTransportError(err) {
		t.Fatal("expected false for HTTP 404 error string")
	}
}

func TestIsTransientTransportError_NotTransient_Generic(t *testing.T) {
	err := errors.New("some unrelated error")
	if isTransientTransportError(err) {
		t.Fatal("expected false for unrelated error")
	}
}

// ── setRetry / New() baseline interaction ────────────────────────────────────

func TestSetRetry_PreservesTransportBaselineWhenUserCountIsLower(t *testing.T) {
	c := resty.New()
	// Simulate what New() does: set the transport baseline.
	c.RetryCount = transportRetryCount

	opt := RetryOption{
		Count:       1, // lower than transportRetryCount (3)
		WaitTime:    time.Second,
		MaxWaitTime: 5 * time.Second,
	}
	setRetry(c, opt)

	if c.RetryCount != transportRetryCount {
		t.Fatalf("expected RetryCount=%d (transport baseline), got %d", transportRetryCount, c.RetryCount)
	}
}

func TestSetRetry_UsesUserCountWhenHigher(t *testing.T) {
	c := resty.New()
	c.RetryCount = transportRetryCount

	opt := RetryOption{
		Count:       10,
		WaitTime:    time.Second,
		MaxWaitTime: 30 * time.Second,
	}
	setRetry(c, opt)

	if c.RetryCount != 10 {
		t.Fatalf("expected RetryCount=10 (user value), got %d", c.RetryCount)
	}
}
