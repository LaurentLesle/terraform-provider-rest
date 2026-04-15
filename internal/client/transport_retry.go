package client

import (
	"errors"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/net/http2"
)

// transportRetry* are the fixed retry parameters applied globally to every
// request, independent of any per-resource retry block. They cover transient
// connection-level failures that occur before any HTTP response body is
// available — situations where the resource-level error_message_regex cannot
// fire.
const (
	transportRetryCount   = 3
	transportRetryWait    = 2 * time.Second
	transportRetryMaxWait = 10 * time.Second
)

// transportRetryCondition is the resty RetryConditionFunc wired in New()
// before any per-resource retry configuration is applied.
func transportRetryCondition(_ *resty.Response, err error) bool {
	return isTransientTransportError(err)
}

// isTransientTransportError returns true if err represents a known transient
// transport-layer failure that is safe to retry:
//   - HTTP/2 RST_STREAM with INTERNAL_ERROR or REFUSED_STREAM error codes
//   - Connection reset by peer / broken pipe
//   - Unexpected EOF mid-stream
//
// It first attempts a typed unwrap via errors.As (golang.org/x/net/http2
// exports http2.StreamError), then falls back to substring matching for
// errors that net/http or resty has wrapped so that errors.As cannot unwrap
// them.
func isTransientTransportError(err error) bool {
	if err == nil {
		return false
	}

	// Typed check: http2.StreamError carries an explicit error code.
	var streamErr http2.StreamError
	if errors.As(err, &streamErr) {
		switch streamErr.Code {
		case http2.ErrCodeInternal, http2.ErrCodeRefusedStream:
			return true
		}
		// Other HTTP/2 error codes are not automatically transient.
		return false
	}

	// String-based fallback for errors that net/http or resty has wrapped in
	// a way that breaks errors.As unwrapping.
	msg := err.Error()
	for _, substr := range []string{
		"stream error",                    // HTTP/2 stream error in string form
		"INTERNAL_ERROR",                  // HTTP/2 error code name in the message
		"REFUSED_STREAM",                  // HTTP/2 error code name in the message
		"connection reset by peer",        // TCP RST from server
		"broken pipe",                     // write to a half-closed connection
		"use of closed network connection", // net.OpError after connection closure
		"unexpected EOF",                  // premature connection close mid-body
	} {
		if strings.Contains(msg, substr) {
			return true
		}
	}

	return false
}
