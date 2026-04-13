package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMatchesAnyRetryRegex(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		patterns []string
		want     bool
	}{
		{"match first", `{"error":"UpdateNotAllowedWhenUpdatingOrDeleting"}`, []string{"UpdateNotAllowedWhenUpdatingOrDeleting"}, true},
		{"match second", `{"error":"AnotherOperationInProgress"}`, []string{"UpdateNotAllowed", "AnotherOperationInProgress"}, true},
		{"no match", `{"error":"SomethingElse"}`, []string{"UpdateNotAllowed"}, false},
		{"empty patterns", `{"error":"anything"}`, []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesAnyRetryRegex([]byte(tc.body), tc.patterns)
			if got != tc.want {
				t.Errorf("matchesAnyRetryRegex(%q, %v) = %v, want %v", tc.body, tc.patterns, got, tc.want)
			}
		})
	}
}

func TestCallWithRetry_NoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"AnotherOperationInProgress"}`))
	}))
	defer srv.Close()

	client := resty.New().SetBaseURL(srv.URL)
	resp, err := callWithRetry(context.Background(), nil, func() (*resty.Response, error) {
		return client.R().Put("/resource")
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode())
	}
	if calls != 1 {
		t.Errorf("expected 1 call with nil retry, got %d", calls)
	}
}

func TestCallWithRetry_SuccessNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := resty.New().SetBaseURL(srv.URL)
	rd := &resourceRetryData{
		ErrorMessageRegex: []string{"AnotherOperationInProgress"},
		IntervalSeconds:   types.Int64Null(),
		MaxAttempts:       types.Int64Value(3),
	}
	resp, err := callWithRetry(context.Background(), rd, func() (*resty.Response, error) {
		return client.R().Put("/resource")
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode())
	}
	if calls != 1 {
		t.Errorf("expected 1 call on success, got %d", calls)
	}
}

func TestCallWithRetry_RetryThenSuccess(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"AnotherOperationInProgress"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := resty.New().SetBaseURL(srv.URL)
	rd := &resourceRetryData{
		ErrorMessageRegex: []string{"AnotherOperationInProgress"},
		IntervalSeconds:   types.Int64Value(0), // 0 sec for fast tests
		MaxAttempts:       types.Int64Value(5),
	}
	resp, err := callWithRetry(context.Background(), rd, func() (*resty.Response, error) {
		return client.R().Put("/resource")
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode())
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestCallWithRetry_NoMatchNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"SomethingUnrelated"}`))
	}))
	defer srv.Close()

	client := resty.New().SetBaseURL(srv.URL)
	rd := &resourceRetryData{
		ErrorMessageRegex: []string{"AnotherOperationInProgress"},
		IntervalSeconds:   types.Int64Value(0),
		MaxAttempts:       types.Int64Value(5),
	}
	resp, err := callWithRetry(context.Background(), rd, func() (*resty.Response, error) {
		return client.R().Put("/resource")
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode())
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no match), got %d", calls)
	}
}

func TestCallWithRetry_MaxAttemptsExhausted(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"AnotherOperationInProgress"}`))
	}))
	defer srv.Close()

	client := resty.New().SetBaseURL(srv.URL)
	rd := &resourceRetryData{
		ErrorMessageRegex: []string{"AnotherOperationInProgress"},
		IntervalSeconds:   types.Int64Value(0),
		MaxAttempts:       types.Int64Value(3),
	}
	resp, err := callWithRetry(context.Background(), rd, func() (*resty.Response, error) {
		return client.R().Put("/resource")
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode())
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (max_attempts=3), got %d", calls)
	}
}
