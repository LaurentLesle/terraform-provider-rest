package client

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

type BuildOption struct {
	Security      SecurityOption
	CookieEnabled bool
	TLSConfig     *tls.Config
	Retry         *RetryOption
}

type SecurityOption interface {
	configureClient(ctx context.Context, client *resty.Client) error
}

type HTTPBasicOption struct {
	Username string
	Password string
}

func (opt HTTPBasicOption) configureClient(_ context.Context, client *resty.Client) error {
	client.SetBasicAuth(opt.Username, opt.Password)
	return nil
}

type HTTPTokenOption struct {
	Token  string
	Scheme string
}

func (opt HTTPTokenOption) configureClient(_ context.Context, client *resty.Client) error {
	client.SetAuthToken(opt.Token)
	if opt.Scheme != "" {
		client.SetAuthScheme(opt.Scheme)
	}
	return nil
}

type APIKeyAuthIn string

const (
	APIKeyAuthInHeader APIKeyAuthIn = "header"
	APIKeyAuthInQuery  APIKeyAuthIn = "query"
	APIKeyAuthInCookie APIKeyAuthIn = "cookie"
)

type APIKeyAuthOpt struct {
	Name  string
	In    APIKeyAuthIn
	Value string
}

type APIKeyAuthOption []APIKeyAuthOpt

func (opt APIKeyAuthOption) configureClient(_ context.Context, client *resty.Client) error {
	for _, key := range opt {
		switch key.In {
		case APIKeyAuthInHeader:
			client.SetHeader(key.Name, key.Value)
		case APIKeyAuthInQuery:
			client.SetQueryParam(key.Name, key.Value)
		case APIKeyAuthInCookie:
			client.SetCookie(&http.Cookie{
				Name:  key.Name,
				Value: key.Value,
			})
		}
	}
	return nil
}

type OAuth2AuthStyle string

const (
	OAuth2AuthStyleInParams OAuth2AuthStyle = "params"
	OAuth2AuthStyleInHeader OAuth2AuthStyle = "header"
)

type OAuth2PasswordOption struct {
	TokenURL string
	ClientId string
	Username string
	Password string

	ClientSecret string
	AuthStyle    OAuth2AuthStyle
	Scopes       []string
}

func (opt OAuth2PasswordOption) configureClient(ctx context.Context, client *resty.Client) error {
	cfg := oauth2.Config{
		ClientID:     opt.ClientId,
		ClientSecret: opt.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: opt.TokenURL,
		},
		Scopes: opt.Scopes,
	}

	switch opt.AuthStyle {
	case OAuth2AuthStyleInHeader:
		cfg.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
	case OAuth2AuthStyleInParams:
		cfg.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	}

	tk, err := cfg.PasswordCredentialsToken(ctx, opt.Username, opt.Password)
	if err != nil {
		return err
	}

	// We use background context here when constructing the client since we are building the client during the provider configuration, where the context is used only for that purpose.
	// Especially, when we use this client, we will set the operation bound context for each request.
	httpClient := client.GetClient()
	ctx = context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)

	// We use background context here when constructing the client since we are building the client during the provider configuration, where the context is used only for that purpose.
	// Especially, when we use this client, we will set the operation bound context for each request.
	*client = *resty.NewWithClient(cfg.Client(ctx, tk))
	return nil
}

type OAuth2ClientCredentialOption struct {
	TokenURL     string
	ClientId     string
	ClientSecret string

	Scopes         []string
	EndpointParams map[string][]string
	AuthStyle      OAuth2AuthStyle
}

func (opt OAuth2ClientCredentialOption) configureClient(_ context.Context, client *resty.Client) error {
	cfg := clientcredentials.Config{
		ClientID:       opt.ClientId,
		ClientSecret:   opt.ClientSecret,
		TokenURL:       opt.TokenURL,
		Scopes:         opt.Scopes,
		EndpointParams: opt.EndpointParams,
	}

	switch opt.AuthStyle {
	case OAuth2AuthStyleInHeader:
		cfg.AuthStyle = oauth2.AuthStyleInHeader
	case OAuth2AuthStyleInParams:
		cfg.AuthStyle = oauth2.AuthStyleInParams
	}

	// We use background context here when constructing the client since we are building the client during the provider configuration, where the context is used only for that purpose.
	// Especially, when we use this client, we will set the operation bound context for each request.
	httpClient := client.GetClient()
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)
	ts := cfg.TokenSource(ctx)
	*client = *resty.NewWithClient(oauth2.NewClient(ctx, ts))
	return nil
}

type OAuth2RefreshTokenOption struct {
	TokenURL     string
	ClientId     string
	RefreshToken string

	ClientSecret string
	AuthStyle    OAuth2AuthStyle
	TokenType    string
	Scopes       []string
}

func DebugLog(format string, args ...any) {
	if os.Getenv("RESTFUL_DEBUG") == "" {
		return
	}
	f, err := os.OpenFile("/tmp/rest-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf(format, args...)
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, format+"\n", args...)
}

func (opt OAuth2RefreshTokenOption) configureClient(_ context.Context, client *resty.Client) error {
	DebugLog("[DEBUG] OAuth2RefreshTokenOption.configureClient: tokenURL=%s clientID=%s scopes=%v", opt.TokenURL, opt.ClientId, opt.Scopes)

	cfg := oauth2.Config{
		ClientID:     opt.ClientId,
		ClientSecret: opt.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: opt.TokenURL,
		},
		Scopes: opt.Scopes,
	}

	switch opt.AuthStyle {
	case OAuth2AuthStyleInHeader:
		cfg.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
	case OAuth2AuthStyleInParams:
		cfg.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	}

	// We use background context here when constructing the client since we are building the client during the provider configuration, where the context is used only for that purpose.
	// Especially, when we use this client, we will set the operation bound context for each request.
	httpClient := client.GetClient()
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)

	// Use a custom token source that includes scopes in the refresh request.
	// The standard oauth2.tokenRefresher does not send scopes during refresh,
	// which causes Azure AD to return tokens with the wrong audience.
	ts := &scopedRefreshTokenSource{
		ctx:          ctx,
		conf:         &cfg,
		refreshToken: opt.RefreshToken,
		tokenType:    opt.TokenType,
	}
	*client = *resty.NewWithClient(oauth2.NewClient(ctx, oauth2.ReuseTokenSource(nil, ts)))
	return nil
}

// scopedRefreshTokenSource is a TokenSource that includes scopes in the
// refresh token request. The standard oauth2.tokenRefresher omits scopes,
// which causes Azure AD to return tokens for the wrong audience when using
// a multi-resource refresh token (e.g. from the Azure CLI).
type scopedRefreshTokenSource struct {
	ctx          context.Context
	conf         *oauth2.Config
	refreshToken string
	tokenType    string
	mu           sync.Mutex
}

func (s *scopedRefreshTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refreshToken == "" {
		return nil, errors.New("oauth2: token expired and refresh token is not set")
	}

	v := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.refreshToken},
		"client_id":     {s.conf.ClientID},
	}
	if s.conf.ClientSecret != "" {
		v.Set("client_secret", s.conf.ClientSecret)
	}
	if len(s.conf.Scopes) > 0 {
		v.Set("scope", strings.Join(s.conf.Scopes, " "))
	}

	DebugLog("[DEBUG] scopedRefreshTokenSource: requesting token from %s with scope=%s", s.conf.Endpoint.TokenURL, v.Get("scope"))

	tk, err := retrieveTokenWithScopes(s.ctx, s.conf.Endpoint.TokenURL, v)
	if err != nil {
		DebugLog("[DEBUG] scopedRefreshTokenSource: error: %v", err)
		return nil, err
	}

	// Decode the access token to check audience (JWT is base64 header.payload.sig)
	parts := strings.SplitN(tk.AccessToken, ".", 3)
	if len(parts) >= 2 {
		// Pad base64 if needed
		payload := parts[1]
		switch len(payload) % 4 {
		case 2:
			payload += "=="
		case 3:
			payload += "="
		}
		decoded, err2 := base64.StdEncoding.DecodeString(payload)
		if err2 == nil {
			var claims map[string]any
			if json.Unmarshal(decoded, &claims) == nil {
				DebugLog("[DEBUG] scopedRefreshTokenSource: token aud=%v", claims["aud"])
			}
		}
	}

	DebugLog("[DEBUG] scopedRefreshTokenSource: got token type=%s expires=%v", tk.TokenType, tk.Expiry)

	// Update refresh token if the server rotated it
	if tk.RefreshToken != "" && tk.RefreshToken != s.refreshToken {
		s.refreshToken = tk.RefreshToken
	}

	if s.tokenType != "" {
		tk.TokenType = s.tokenType
	}

	return tk, nil
}

// retrieveTokenWithScopes performs an OAuth2 token request that includes the
// scope parameter. This is needed because Go's oauth2.tokenRefresher omits
// scopes during refresh, which causes Azure AD to return tokens with the
// wrong audience when using multi-resource refresh tokens.
func retrieveTokenWithScopes(ctx context.Context, tokenURL string, v url.Values) (*oauth2.Token, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := http.DefaultClient
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok {
		httpClient = c
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth2: token request returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("oauth2: failed to parse token response: %w", err)
	}

	tk := &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		RefreshToken: tokenResp.RefreshToken,
	}
	if tokenResp.ExpiresIn > 0 {
		tk.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tk, nil
}
