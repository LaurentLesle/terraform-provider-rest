package provider

import (
	"fmt"
	"net/http"

	"github.com/go-resty/resty/v2"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func DiagToError(diags diag.Diagnostics) error {
	if !diags.HasError() {
		return nil
	}

	var err error
	for _, ed := range diags.Errors() {
		err = multierror.Append(err, fmt.Errorf("%s: %s", ed.Summary(), ed.Detail()))
	}
	return err
}

// apiErrorDetail returns the response body as a string. When the response is
// HTTP 429 (TooManyRequests) it appends a hint about the Retry-After value and
// suggests increasing the provider's retry configuration.
func apiErrorDetail(resp *resty.Response) string {
	detail := string(resp.Body())
	if resp.StatusCode() != http.StatusTooManyRequests {
		return detail
	}
	if ra := resp.Header().Get("Retry-After"); ra != "" {
		detail += fmt.Sprintf("\n\nThe server set Retry-After: %s.", ra)
	}
	detail += "\nAll retries exhausted. Consider increasing `max_wait_in_sec` and/or `count` in the provider's client.retry configuration."
	return detail
}
