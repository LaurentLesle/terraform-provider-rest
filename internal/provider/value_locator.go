package provider

import (
	"fmt"
	"strings"

	"github.com/LaurentLesle/terraform-provider-rest/internal/client"
	"github.com/LaurentLesle/terraform-provider-rest/internal/exparam"
)

func validateLocator(locator string) error {
	if locator == "code" {
		return nil
	}
	l, r, ok := strings.Cut(locator, ".")
	if !ok {
		return fmt.Errorf("locator doesn't contain `.`: %s", locator)
	}
	if r == "" {
		return fmt.Errorf("empty right hand value for locator: %s", locator)
	}
	switch l {
	case "exact", "header", "body":
		return nil
	default:
		return fmt.Errorf("unknown locator key: %s", l)
	}
}

// expandValueLocator resolves a locator string into a ValueLocator.
// When body is non-nil, exparam placeholders in the locator value are expanded.
// When body is nil, ExpandBody is a no-op and the raw value is used.
func expandValueLocator(locator string, body []byte) (client.ValueLocator, error) {
	if locator == "code" {
		return client.CodeLocator{}, nil
	}
	l, r, ok := strings.Cut(locator, ".")
	if !ok {
		return nil, fmt.Errorf("locator doesn't contain `.`: %s", locator)
	}
	if r == "" {
		return nil, fmt.Errorf("empty right hand value for locator: %s", locator)
	}
	switch l {
	case "exact":
		return client.ExactLocator(r), nil
	case "header":
		rr, err := exparam.ExpandBody(r, body)
		if err != nil {
			return nil, fmt.Errorf("expand param of %q: %v", r, err)
		}
		return client.HeaderLocator(rr), nil
	case "body":
		rr, err := exparam.ExpandBody(r, body)
		if err != nil {
			return nil, fmt.Errorf("expand param of %q: %v", r, err)
		}
		return client.BodyLocator(rr), nil
	default:
		return nil, fmt.Errorf("unknown locator key: %s", l)
	}
}
