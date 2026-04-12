package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/LaurentLesle/terraform-provider-rest/internal/acceptance"
)

const (
	restfulMAASURL    = "RESTFUL_MAAS_URL"
	restfulMAASAPIKey = "RESTFUL_MAAS_API_KEY"

	mockConsumerKey   = "testconsumer"
	mockConsumerToken = "testtoken"
	mockTokenSecret   = "testsecret"
)

// maasData holds connection details for either a real MAAS instance or the
// in-process mock. newMaasData always returns a usable server — no t.Skip.
type maasData struct {
	url           string
	consumerKey   string
	consumerToken string
	tokenSecret   string
	rd            acceptance.Rd
}

func newMaasData(t *testing.T) maasData {
	t.Helper()
	if rawURL := os.Getenv(restfulMAASURL); rawURL != "" {
		apiKey := os.Getenv(restfulMAASAPIKey)
		if apiKey == "" {
			t.Fatalf("%s is set but %s is missing", restfulMAASURL, restfulMAASAPIKey)
		}
		parts := strings.SplitN(apiKey, ":", 3)
		if len(parts) != 3 {
			t.Fatalf("%s must be in consumer_key:consumer_token:token_secret format", restfulMAASAPIKey)
		}
		return maasData{
			url:           rawURL,
			consumerKey:   parts[0],
			consumerToken: parts[1],
			tokenSecret:   parts[2],
			rd:            acceptance.NewRd(),
		}
	}

	// No env var — start the in-process mock. Always runs in CI.
	srv := startMaasMockServer(t, mockConsumerKey, mockConsumerToken, mockTokenSecret)
	t.Cleanup(srv.Close)
	return maasData{
		url:           srv.URL,
		consumerKey:   mockConsumerKey,
		consumerToken: mockConsumerToken,
		tokenSecret:   mockTokenSecret,
		rd:            acceptance.NewRd(),
	}
}

// ── Provider config helpers ──────────────────────────────────────────────────

func (d maasData) providerBlock() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
  security = {
    oauth1 = {
      consumer_key   = %q
      consumer_token = %q
      token_secret   = %q
    }
  }
}
`, d.url, d.consumerKey, d.consumerToken, d.tokenSecret)
}

// ── Test configs ─────────────────────────────────────────────────────────────

func (d maasData) fabricConfig(name string) string {
	return d.providerBlock() + fmt.Sprintf(`
resource "rest_resource" "test" {
  path      = "/api/2.0/fabrics/"
  read_path = "$(path)$(body.id)"
  header = {
    "Content-Type" = "application/x-www-form-urlencoded"
  }
  body = {
    name = %q
  }
}
`, name)
}

func (d maasData) subnetConfig(cidr, name string) string {
	return d.providerBlock() + fmt.Sprintf(`
resource "rest_resource" "test" {
  path      = "/api/2.0/subnets/"
  read_path = "$(path)$(body.id)"
  header = {
    "Content-Type" = "application/x-www-form-urlencoded"
  }
  body = {
    cidr       = %q
    name       = %q
    gateway_ip = "192.168.1.1"
  }
}
`, cidr, name)
}

// ── Tests ────────────────────────────────────────────────────────────────────

// TestResource_MAAS_Fabric verifies full CRUD lifecycle for a MAAS fabric.
// When RESTFUL_MAAS_URL is unset the in-process mock is used — always runs in CI.
func TestResource_MAAS_Fabric(t *testing.T) {
	addr := "rest_resource.test"
	d := newMaasData(t)

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.fabricConfig("fabric-alpha"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("name"), knownvalue.StringExact("fabric-alpha")),
				},
			},
			{
				Config: d.fabricConfig("fabric-beta"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("name"), knownvalue.StringExact("fabric-beta")),
				},
			},
		},
	})
}

// TestResource_MAAS_Subnet verifies that form-encoded bodies are sent and parsed
// correctly for a MAAS subnet resource.
func TestResource_MAAS_Subnet(t *testing.T) {
	addr := "rest_resource.test"
	d := newMaasData(t)

	resource.ParallelTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.subnetConfig("10.0.0.0/24", "mgmt"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("cidr"), knownvalue.StringExact("10.0.0.0/24")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("name"), knownvalue.StringExact("mgmt")),
				},
			},
			{
				Config: d.subnetConfig("10.0.0.0/24", "mgmt-renamed"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("output").AtMapKey("name"), knownvalue.StringExact("mgmt-renamed")),
				},
			},
		},
	})
}

// TestResource_MAAS_OAuth1Rejected verifies that requests without a valid
// OAuth1 Authorization header are rejected with HTTP 401.
func TestResource_MAAS_OAuth1Rejected(t *testing.T) {
	srv := startMaasMockServer(t, mockConsumerKey, mockConsumerToken, mockTokenSecret)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest("GET", srv.URL+"/api/2.0/fabrics/", nil)
	// No Authorization header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ── In-process MAAS mock server ──────────────────────────────────────────────

type maasStore struct {
	mu      sync.Mutex
	fabrics map[int]map[string]any
	subnets map[int]map[string]any
	nextID  map[string]int
}

func newMaasStore() *maasStore {
	return &maasStore{
		fabrics: make(map[int]map[string]any),
		subnets: make(map[int]map[string]any),
		nextID:  map[string]int{"fabrics": 1, "subnets": 1},
	}
}

func (s *maasStore) allocID(resource string) int {
	id := s.nextID[resource]
	s.nextID[resource]++
	return id
}

func startMaasMockServer(t *testing.T, consumerKey, consumerToken, tokenSecret string) *httptest.Server {
	t.Helper()
	store := newMaasStore()

	mux := http.NewServeMux()

	// ── Auth middleware ───────────────────────────────────────────────────────
	check := func(w http.ResponseWriter, r *http.Request) bool {
		if !verifyOAuth1PLAINTEXT(r, consumerKey, consumerToken, tokenSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}

	writeJSON := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}

	// ── Fabrics collection ────────────────────────────────────────────────────
	mux.HandleFunc("/api/2.0/fabrics/", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		// Strip prefix to check if this is a collection or singular request
		tail := strings.TrimPrefix(r.URL.Path, "/api/2.0/fabrics/")
		tail = strings.Trim(tail, "/")

		if tail == "" {
			// Collection endpoint
			switch r.Method {
			case http.MethodGet:
				store.mu.Lock()
				list := make([]map[string]any, 0, len(store.fabrics))
				for _, f := range store.fabrics {
					list = append(list, f)
				}
				store.mu.Unlock()
				writeJSON(w, http.StatusOK, list)

			case http.MethodPost:
				body := parseFormOrJSON(r)
				store.mu.Lock()
				id := store.allocID("fabrics")
				fabric := map[string]any{
					"id":           id,
					"name":         body["name"],
					"description":  body["description"],
					"resource_uri": fmt.Sprintf("/MAAS/api/2.0/fabrics/%d/", id),
				}
				store.fabrics[id] = fabric
				store.mu.Unlock()
				writeJSON(w, http.StatusOK, fabric)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		// Singular endpoint: /api/2.0/fabrics/{id}
		id, err := strconv.Atoi(tail)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			store.mu.Lock()
			fabric, ok := store.fabrics[id]
			store.mu.Unlock()
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, fabric)

		case http.MethodPut:
			body := parseFormOrJSON(r)
			store.mu.Lock()
			fabric, ok := store.fabrics[id]
			if !ok {
				store.mu.Unlock()
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if v, ok := body["name"]; ok {
				fabric["name"] = v
			}
			if v, ok := body["description"]; ok {
				fabric["description"] = v
			}
			store.fabrics[id] = fabric
			store.mu.Unlock()
			writeJSON(w, http.StatusOK, fabric)

		case http.MethodDelete:
			store.mu.Lock()
			delete(store.fabrics, id)
			store.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// ── Subnets collection ────────────────────────────────────────────────────
	mux.HandleFunc("/api/2.0/subnets/", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		tail := strings.TrimPrefix(r.URL.Path, "/api/2.0/subnets/")
		tail = strings.Trim(tail, "/")

		if tail == "" {
			switch r.Method {
			case http.MethodGet:
				store.mu.Lock()
				list := make([]map[string]any, 0, len(store.subnets))
				for _, s := range store.subnets {
					list = append(list, s)
				}
				store.mu.Unlock()
				writeJSON(w, http.StatusOK, list)

			case http.MethodPost:
				body := parseFormOrJSON(r)
				store.mu.Lock()
				id := store.allocID("subnets")
				subnet := map[string]any{
					"id":           id,
					"cidr":         body["cidr"],
					"name":         body["name"],
					"gateway_ip":   body["gateway_ip"],
					"resource_uri": fmt.Sprintf("/MAAS/api/2.0/subnets/%d/", id),
				}
				store.subnets[id] = subnet
				store.mu.Unlock()
				writeJSON(w, http.StatusOK, subnet)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		id, err := strconv.Atoi(tail)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			store.mu.Lock()
			subnet, ok := store.subnets[id]
			store.mu.Unlock()
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, subnet)

		case http.MethodPut:
			body := parseFormOrJSON(r)
			store.mu.Lock()
			subnet, ok := store.subnets[id]
			if !ok {
				store.mu.Unlock()
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			for _, field := range []string{"cidr", "name", "gateway_ip"} {
				if v, ok := body[field]; ok {
					subnet[field] = v
				}
			}
			store.subnets[id] = subnet
			store.mu.Unlock()
			writeJSON(w, http.StatusOK, subnet)

		case http.MethodDelete:
			store.mu.Lock()
			delete(store.subnets, id)
			store.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(mux)
}

// verifyOAuth1PLAINTEXT checks that the request carries a valid OAuth 1.0
// PLAINTEXT Authorization header for the given credentials.
func verifyOAuth1PLAINTEXT(r *http.Request, consumerKey, consumerToken, tokenSecret string) bool {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "OAuth ") {
		return false
	}

	params := parseOAuthHeader(authHeader)

	if params["oauth_signature_method"] != "PLAINTEXT" {
		return false
	}
	if params["oauth_consumer_key"] != consumerKey {
		return false
	}
	if params["oauth_token"] != consumerToken {
		return false
	}
	// PLAINTEXT signature is &<url-encoded token_secret>
	expectedSig := "&" + url.QueryEscape(tokenSecret)
	if params["oauth_signature"] != expectedSig {
		return false
	}
	return true
}

// parseOAuthHeader parses an OAuth Authorization header into a key→value map.
// Values are URL-decoded. Example input:
//
//	OAuth oauth_version="1.0", oauth_consumer_key="foo", oauth_signature="&bar"
func parseOAuthHeader(header string) map[string]string {
	result := make(map[string]string)
	body := strings.TrimPrefix(header, "OAuth ")
	for _, part := range strings.Split(body, ", ") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		decoded, err := url.QueryUnescape(val)
		if err != nil {
			decoded = val
		}
		result[key] = decoded
	}
	return result
}

// parseFormOrJSON reads the request body as either application/x-www-form-urlencoded
// or JSON and returns a flat map[string]string.
func parseFormOrJSON(r *http.Request) map[string]string {
	ct := r.Header.Get("Content-Type")
	result := make(map[string]string)

	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err == nil {
			for k, vs := range r.Form {
				if len(vs) > 0 {
					result[k] = vs[0]
				}
			}
		}
		return result
	}

	// Fall back to JSON
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err == nil {
		for k, v := range m {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}
