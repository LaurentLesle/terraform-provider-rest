package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	defaultRetryIntervalSec = 10
	defaultRetryMaxAttempts = 6
)

// callWithRetry executes fn. If the response is non-success and its body matches any
// error_message_regex pattern, it waits interval_seconds and retries, up to max_attempts total.
// When rd is nil, fn is called exactly once.
func callWithRetry(ctx context.Context, rd *resourceRetryData, fn func() (*resty.Response, error)) (*resty.Response, error) {
	if rd == nil {
		return fn()
	}

	intervalSec := int64(defaultRetryIntervalSec)
	if !rd.IntervalSeconds.IsNull() && !rd.IntervalSeconds.IsUnknown() {
		intervalSec = rd.IntervalSeconds.ValueInt64()
	}
	maxAttempts := int64(defaultRetryMaxAttempts)
	if !rd.MaxAttempts.IsNull() && !rd.MaxAttempts.IsUnknown() {
		maxAttempts = rd.MaxAttempts.ValueInt64()
	}

	for attempt := int64(1); ; attempt++ {
		resp, err := fn()
		if err != nil {
			return resp, err
		}
		if resp.IsSuccess() || attempt >= maxAttempts {
			return resp, nil
		}
		if !matchesAnyRetryRegex(resp.Body(), rd.ErrorMessageRegex) {
			return resp, nil
		}
		tflog.Info(ctx, "Retrying after error_message_regex match", map[string]interface{}{
			"attempt":          attempt,
			"max_attempts":     maxAttempts,
			"interval_seconds": intervalSec,
		})
		select {
		case <-ctx.Done():
			return resp, ctx.Err()
		case <-time.After(time.Duration(intervalSec) * time.Second):
		}
	}
}

func matchesAnyRetryRegex(body []byte, patterns []string) bool {
	s := string(body)
	for _, p := range patterns {
		if matched, _ := regexp.MatchString(p, s); matched {
			return true
		}
	}
	return false
}
