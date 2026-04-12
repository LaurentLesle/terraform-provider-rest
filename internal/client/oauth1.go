package client

import (
	"context"
	"net/http"

	"github.com/go-resty/resty/v2"
	"github.com/LaurentLesle/terraform-provider-rest/internal/oauth1"
)

// OAuth1Option implements SecurityOption for 0-legged OAuth 1.0 authentication.
// Used by the MAAS API which issues API keys in consumer_key:token:secret format.
type OAuth1Option struct {
	ConsumerKey     string
	ConsumerToken   string
	TokenSecret     string
	SignatureMethod string // "PLAINTEXT" (default) or "HMAC-SHA1"
}

func (opt OAuth1Option) configureClient(_ context.Context, c *resty.Client) error {
	inner := c.GetClient()
	base := inner.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	inner.Transport = &oauth1Transport{
		base:            base,
		consumerKey:     opt.ConsumerKey,
		consumerToken:   opt.ConsumerToken,
		tokenSecret:     opt.TokenSecret,
		signatureMethod: opt.SignatureMethod,
	}
	return nil
}

// oauth1Transport wraps an http.RoundTripper and injects a fresh OAuth 1.0
// Authorization header on every request. The header is generated at
// RoundTrip time so that nonce and timestamp are unique per request.
type oauth1Transport struct {
	base            http.RoundTripper
	consumerKey     string
	consumerToken   string
	tokenSecret     string
	signatureMethod string
}

func (t *oauth1Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	sm := t.signatureMethod
	if sm == "" {
		sm = oauth1.PLAINTEXT
	}
	r.Header.Set("Authorization", oauth1.BuildHeader(
		t.consumerKey, t.consumerToken, t.tokenSecret, sm, req.Method, req.URL.String(),
	))
	return t.base.RoundTrip(r)
}
