// Package oauth1 provides OAuth 1.0 header building utilities for 0-legged
// authentication (no request token exchange). Used by the MAAS API integration.
package oauth1

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	PLAINTEXT = "PLAINTEXT"
	HMACSHA1  = "HMAC-SHA1"
)

// BuildHeader returns an OAuth 1.0 Authorization header value for the given
// signature method. For MAAS, use PLAINTEXT. method and rawURL are only
// required for HMAC-SHA1 (ignored for PLAINTEXT).
func BuildHeader(consumerKey, consumerToken, tokenSecret, signatureMethod, method, rawURL string) string {
	n := newNonce()
	ts := fmt.Sprintf("%d", time.Now().Unix())

	var sig string
	switch signatureMethod {
	case HMACSHA1:
		sig = hmacSHA1Signature(consumerKey, consumerToken, tokenSecret, method, rawURL, n, ts)
	default:
		signatureMethod = PLAINTEXT
		sig = "&" + url.QueryEscape(tokenSecret)
	}

	return fmt.Sprintf(
		`OAuth oauth_version="1.0", oauth_signature_method="%s", oauth_consumer_key="%s", oauth_token="%s", oauth_signature="%s", oauth_nonce="%s", oauth_timestamp="%s"`,
		signatureMethod,
		url.QueryEscape(consumerKey),
		url.QueryEscape(consumerToken),
		sig,
		n,
		ts,
	)
}

// newNonce generates a cryptographically random 16-byte hex nonce.
func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// hmacSHA1Signature computes an OAuth 1.0 HMAC-SHA1 signature for 0-legged
// auth (consumer secret is empty). Only URL query parameters are included in
// the parameter set; request body parameters are not (sufficient for GET/DELETE).
func hmacSHA1Signature(consumerKey, consumerToken, tokenSecret, method, rawURL, nonce, timestamp string) string {
	// Build OAuth parameter set
	params := map[string]string{
		"oauth_version":          "1.0",
		"oauth_signature_method": HMACSHA1,
		"oauth_consumer_key":     consumerKey,
		"oauth_token":            consumerToken,
		"oauth_nonce":            nonce,
		"oauth_timestamp":        timestamp,
	}

	// Include URL query parameters
	if u, err := url.Parse(rawURL); err == nil {
		for k, vs := range u.Query() {
			params[k] = vs[0]
		}
		// Strip query and fragment for base URL
		u.RawQuery = ""
		u.Fragment = ""
		rawURL = u.String()
	}

	// Normalize parameters: percent-encode key=value, sort, join with &
	pairs := make([]string, 0, len(params))
	for k, v := range params {
		pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
	}
	sort.Strings(pairs)
	normalizedParams := strings.Join(pairs, "&")

	// Base string: METHOD&encoded_url&encoded_params
	baseString := strings.ToUpper(method) + "&" +
		url.QueryEscape(strings.ToLower(rawURL)) + "&" +
		url.QueryEscape(normalizedParams)

	// Signing key: consumer_secret&token_secret (consumer_secret is empty for 0-legged)
	signingKey := "&" + url.QueryEscape(tokenSecret)

	mac := hmac.New(sha1.New, []byte(signingKey))
	_, _ = mac.Write([]byte(baseString))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
