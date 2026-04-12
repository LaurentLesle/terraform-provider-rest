package functions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	oauth1pkg "github.com/LaurentLesle/terraform-provider-rest/internal/oauth1"
)

// ProviderTokens holds the API tokens and settings for external resource validation.
type ProviderTokens struct {
	ARMToken        string
	ARMTenantTokens map[string]string // tenant_id → ARM token for cross-tenant access
	GraphToken      string
	GitHubToken     string
	MaasURL         string
	MaasAPIKey      string // consumer_key:consumer_token:token_secret
	FailOnWarning   bool
}

// ConfigProvider is implemented by the provider to supply tokens to functions.
type ConfigProvider interface {
	GetTokens() *ProviderTokens
}

var _ function.Function = &ValidateExternalsFunction{}

// ValidateExternalsFunction validates and enriches external resource references
// by performing read-only API calls. Validation is schema-driven via a schema
// registry or inline _schema keys.
//
// Entries with _exported_attributes are enriched: the function fetches live data
// from the API and adds the requested attributes to the returned map.
type ValidateExternalsFunction struct {
	configProvider ConfigProvider
}

// NewValidateExternalsWithConfig creates the function with a ConfigProvider.
func NewValidateExternalsWithConfig(cp ConfigProvider) function.Function {
	return &ValidateExternalsFunction{configProvider: cp}
}

// categorySchema describes how to validate/enrich entries in a category.
type categorySchema struct {
	API          string
	Path         string
	APIVersion   string
	SearchFilter string
	Attributes   []string // explicit allowed attributes; nil = infer from placeholders
	AllowAny     bool     // true when attributes: "*"
}

// apiEndpoint maps an API name to its base URL and auth scheme.
type apiEndpoint struct {
	baseURL    string
	authScheme string // "Bearer" or "token"
}

var apiEndpoints = map[string]apiEndpoint{
	"arm":    {baseURL: "https://management.azure.com", authScheme: "Bearer"},
	"graph":  {baseURL: "https://graph.microsoft.com", authScheme: "Bearer"},
	"github": {baseURL: "https://api.github.com", authScheme: "token"},
}

func (f *ValidateExternalsFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "validate_externals"
}

func (f *ValidateExternalsFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Validate and enrich external resource references via read-only API calls",
		Description: `Takes the externals map and validates each entry against live APIs using
the tokens configured on the provider.

Schema lookup order:
  1. schema_registry (second parameter) — decoded YAML map of category → schema
  2. Inline _schema key inside each externals category

Entries may include _exported_attributes to fetch live data from the API:
  _exported_attributes: [field1, field2]   — fetch specific fields
  _exported_attributes: ["*"]              — fetch all scalar fields

When _exported_attributes is present, the function does a GET, parses the JSON
response, converts field names to snake_case, and adds them to the entry.
User-provided attributes take precedence over API-fetched values.

Schema fields:
  api: arm | graph | github
  path: "/subscriptions/{subscription_id}/resourcegroups/{name}"
  api_version: "2022-09-01"           # optional
  search_filter: "field eq '{attr}'"  # optional
  attributes: [list] or "*"           # optional — allowed attributes

Categories without a schema pass through unchanged.
Validation/enrichment is skipped when the required token is not configured.

Returns the (possibly enriched) externals map. Errors on HTTP 404.`,
		Parameters: []function.Parameter{
			function.DynamicParameter{
				Name:        "externals",
				Description: "The externals map. Top-level keys are categories, each containing resource entries.",
			},
			function.DynamicParameter{
				Name:        "schema_registry",
				Description: "Optional schema registry map (e.g. yamldecode(file('externals_schema.yaml'))). When null/empty, only inline _schema keys are used.",
			},
		},
		Return: function.DynamicReturn{},
	}
}

func (f *ValidateExternalsFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var externalsVal types.Dynamic
	var registryVal types.Dynamic

	resp.Error = function.ConcatFuncErrors(
		req.Arguments.Get(ctx, &externalsVal, &registryVal),
	)
	if resp.Error != nil {
		return
	}

	underlying := externalsVal.UnderlyingValue()

	// Nil/null/empty → pass through
	if underlying == nil || underlying.IsNull() || underlying.IsUnknown() {
		resp.Error = function.ConcatFuncErrors(
			resp.Result.Set(ctx, externalsVal),
		)
		return
	}

	// Parse schema registry (second param) — may be null
	var registry map[string]attr.Value
	if ru := registryVal.UnderlyingValue(); ru != nil && !ru.IsNull() && !ru.IsUnknown() {
		registry = extractEntries(ru)
	}

	// Get tokens from provider config, with env var fallback.
	tokens := resolveTokens(f.configProvider)

	enrichedData, structErrors, err := ValidateAndEnrich(underlying, registry, tokens)
	if err != "" {
		resp.Error = function.NewFuncError(err)
		return
	}
	if len(structErrors) > 0 {
		resp.Error = function.NewFuncError(
			"External resource validation failed:\n  " + strings.Join(structErrors, "\n  "),
		)
		return
	}

	// When fail_on_warning is true, collect _warning entries and raise a hard error
	if tokens.FailOnWarning {
		var warnErrors []string
		for catKey, entries := range enrichedData {
			for entryKey, attrs := range entries {
				if w, ok := attrs["_warning"]; ok {
					warnErrors = append(warnErrors, fmt.Sprintf("externals.%s.%s: %s", catKey, entryKey, w))
				}
			}
		}
		if len(warnErrors) > 0 {
			slices.Sort(warnErrors)
			resp.Error = function.NewFuncError(
				"External resource validation failed (fail_on_warning=true):\n  " + strings.Join(warnErrors, "\n  "),
			)
			return
		}
	}

	result, diags := buildEnrichedDynamic(enrichedData)
	if diags.HasError() {
		resp.Error = function.NewFuncError(
			fmt.Sprintf("Failed to build enriched externals: %s", diags))
		return
	}
	resp.Error = function.ConcatFuncErrors(
		resp.Result.Set(ctx, result),
	)
}

// resolveTokens gets tokens from the provider config with env var fallback.
func resolveTokens(cp ConfigProvider) *ProviderTokens {
	var tokens *ProviderTokens
	if cp != nil {
		tokens = cp.GetTokens()
	}
	if tokens == nil {
		tokens = &ProviderTokens{}
	}
	if tokens.ARMToken == "" {
		tokens.ARMToken = os.Getenv("TF_VAR_azure_access_token")
	}
	if tokens.GraphToken == "" {
		tokens.GraphToken = os.Getenv("TF_VAR_graph_access_token")
	}
	if tokens.GitHubToken == "" {
		tokens.GitHubToken = os.Getenv("TF_VAR_github_token")
	}
	if tokens.MaasURL == "" {
		tokens.MaasURL = os.Getenv("TF_VAR_maas_url")
	}
	if tokens.MaasAPIKey == "" {
		tokens.MaasAPIKey = os.Getenv("TF_VAR_maas_api_key")
	}
	if !tokens.FailOnWarning {
		tokens.FailOnWarning = os.Getenv("TF_VAR_fail_on_warning") == "true"
	}
	// Env fallback for tenant tokens: JSON map in TF_VAR_arm_tenant_tokens
	if len(tokens.ARMTenantTokens) == 0 {
		if envJSON := os.Getenv("TF_VAR_arm_tenant_tokens"); envJSON != "" {
			var m map[string]string
			if err := json.Unmarshal([]byte(envJSON), &m); err == nil {
				tokens.ARMTenantTokens = m
			}
		}
	}
	return tokens
}

// ValidateAndEnrich is the core logic shared by the function and data source.
// Returns enrichedData, structural errors, and a fatal error string (empty if none).
func ValidateAndEnrich(underlying attr.Value, registry map[string]attr.Value, tokens *ProviderTokens) (map[string]map[string]map[string]string, []string, string) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Build a per-call endpoints map so MAAS (self-hosted) can be injected
	// without mutating the global apiEndpoints.
	endpoints := make(map[string]apiEndpoint, len(apiEndpoints)+1)
	for k, v := range apiEndpoints {
		endpoints[k] = v
	}
	if tokens.MaasURL != "" {
		endpoints["maas"] = apiEndpoint{baseURL: tokens.MaasURL, authScheme: "oauth1"}
	}

	categories := extractEntries(underlying)
	var structErrors []string

	enrichedData := make(map[string]map[string]map[string]string)

	// ── Pre-pass: collect user-provided attributes from all categories ───
	// This allows ref:existing. to resolve across categories regardless of
	// alphabetical sort order (e.g. azure_billing_accounts can reference
	// azure_tenants even though it sorts earlier).
	for _, categoryKey := range sortedKeys(categories) {
		categoryVal := categories[categoryKey]
		entries := extractEntries(categoryVal)
		preEntries := make(map[string]map[string]string)
		for entryKey, entryVal := range entries {
			if entryKey == "_schema" {
				continue
			}
			attrs := extractAttrs(entryVal)
			base := make(map[string]string)
			for k := range attrs {
				if k == "_exported_attributes" || k == "_schema" || k == "_tenant" {
					continue
				}
				if s, ok := getStringAttr(attrs, k); ok {
					base[k] = s
				}
			}
			preEntries[entryKey] = base
		}
		enrichedData[categoryKey] = preEntries
	}

	// ── Main pass: validate, enrich via API calls, overlay results ───────
	// Process categories in sorted order so ref:existing. references
	// resolve deterministically against already-enriched categories.
	for _, categoryKey := range sortedKeys(categories) {
		categoryVal := categories[categoryKey]
		entries := extractEntries(categoryVal)
		schema := lookupSchema(categoryKey, entries, registry)

		enrichedEntries := make(map[string]map[string]string)

		for entryKey, entryVal := range entries {
			if entryKey == "_schema" {
				continue
			}

			attrs := extractAttrs(entryVal)

			// Resolve ref:existing. expressions in attrs so that downstream
			// URL building (substitutePlaceholders) sees resolved values.
			resolveExistingRefsInAttrs(attrs, enrichedData)

			// Collect base string attributes (skip metadata keys)
			baseAttrs := make(map[string]string)
			for k := range attrs {
				if k == "_exported_attributes" || k == "_schema" || k == "_tenant" {
					continue
				}
				if s, ok := getStringAttr(attrs, k); ok {
					baseAttrs[k] = s
				}
			}

			// Merge entry-level _schema override with category schema
			entrySchema := mergeEntrySchema(schema, attrs)

			// Parse _exported_attributes
			exportList, isWildcard := parseExportedAttrs(attrs)
			hasExport := len(exportList) > 0 || isWildcard

			// Default: when the schema declares attributes and the entry
			// doesn't specify _exported_attributes, inherit from the schema.
			//   attributes: "*"          → behave as _exported_attributes: ["*"]
			//   attributes: [id, name]   → behave as _exported_attributes: [id, name]
			// This lets entries be enriched automatically without repeating
			// _exported_attributes on every entry.
			if !hasExport && entrySchema != nil {
				if entrySchema.AllowAny {
					isWildcard = true
					hasExport = true
				} else if len(entrySchema.Attributes) > 0 {
					exportList = entrySchema.Attributes
					hasExport = true
				}
			}

			// ── Structural validation ───────────────────────────────────
			if entrySchema != nil {
				allowed := entrySchema.Attributes
				if len(allowed) == 0 && !entrySchema.AllowAny {
					allowed = extractPlaceholders(entrySchema.Path, entrySchema.SearchFilter)
				}

				// Check unknown attributes (skip _exported_attributes, _schema)
				if len(allowed) > 0 && !entrySchema.AllowAny {
					allowedSet := make(map[string]bool, len(allowed)+2)
					for _, a := range allowed {
						allowedSet[a] = true
					}
					allowedSet["_exported_attributes"] = true
					allowedSet["_schema"] = true
					allowedSet["_tenant"] = true
					allowedSet["id"] = true // always allow id (shortcut)
					for attrKey := range attrs {
						if !allowedSet[attrKey] {
							structErrors = append(structErrors, fmt.Sprintf(
								"externals.%s.%s: unknown attribute %q (allowed: %s)",
								categoryKey, entryKey, attrKey, strings.Join(allowed, ", ")))
						}
					}
				}
			}

			// ── API call (validation or enrichment) ─────────────────────
			if entrySchema != nil {
				// Special handling: token_claims decodes JWT without any API call
				if entrySchema.API == "token_claims" {
					// Pick token: prefer tenant-specific if _tenant is set
					jwtToken := ""
					if tenantID, ok := getStringAttr(attrs, "_tenant"); ok && tenantID != "" {
						if t, ok := tokens.ARMTenantTokens[tenantID]; ok && t != "" {
							jwtToken = t
						}
					}
					if jwtToken == "" {
						jwtToken = tokens.ARMToken
					}
					if jwtToken == "" {
						jwtToken = tokens.GraphToken
					}
					if jwtToken != "" {
						claims, err := decodeJWTClaims(jwtToken)
						if err != nil {
							baseAttrs["_warning"] = fmt.Sprintf("JWT decode failed: %s", err)
						} else {
							if isWildcard {
								for ck, cv := range claims {
									if _, exists := baseAttrs[ck]; !exists {
										baseAttrs[ck] = cv
									}
								}
							} else if len(exportList) > 0 {
								for _, desired := range exportList {
									if cv, ok := claims[desired]; ok {
										if _, exists := baseAttrs[desired]; !exists {
											baseAttrs[desired] = cv
										}
									}
								}
							}
						}
					} else {
						baseAttrs["_warning"] = "no token available for caller_context introspection"
					}
				} else {
					ep, epOK := endpoints[entrySchema.API]
					token := ""
					if epOK {
						token = tokenForAPIWithTenant(tokens, entrySchema.API, attrs)
					}

					if epOK && token != "" {
						url := buildSchemaURL(ep.baseURL, entrySchema, attrs)
						searchFilter := resolveSearchFilter(entrySchema, attrs)
						if url != "" {
							if hasExport {
								// Enrichment: fetch JSON and extract attributes
								jsonBody, err := fetchJSON(client, url, ep.authScheme, token)
								if err != nil {
									baseAttrs["_warning"] = fmt.Sprintf("%s (enrichment skipped)", err)
								} else if jsonBody != nil {
									// Handle list responses with client-side filtering
									resultObj := extractFirstResult(jsonBody, searchFilter)
									if resultObj == nil {
										baseAttrs["_warning"] = "no matching resource found (enrichment skipped)"
									} else {
										flattened := flattenJSONResponse(resultObj)
										if isWildcard {
											for fk, fv := range flattened {
												if _, exists := baseAttrs[fk]; !exists {
													baseAttrs[fk] = fv
												}
											}
										} else {
											for _, desired := range exportList {
												if fv, ok := flattened[desired]; ok {
													if _, exists := baseAttrs[desired]; !exists {
														baseAttrs[desired] = fv
													}
												}
											}
										}
									}
								}
								// jsonBody == nil means auth failure → skip silently
							} else {
								// Pure validation: just check existence
								if err := doGET(client, url, ep.authScheme, token); err != nil {
									baseAttrs["_warning"] = fmt.Sprintf("%s (validation skipped)", err)
								}
							}
						}
					}
				} // end else (non-token_claims API)
			}

			enrichedEntries[entryKey] = baseAttrs
		}

		enrichedData[categoryKey] = enrichedEntries
	}

	return enrichedData, structErrors, ""
}

// CollectWarnings returns sorted warning messages from enriched data entries.
func CollectWarnings(enrichedData map[string]map[string]map[string]string) []string {
	var warnings []string
	for catKey, entries := range enrichedData {
		for entryKey, attrs := range entries {
			if w, ok := attrs["_warning"]; ok {
				warnings = append(warnings, fmt.Sprintf("externals.%s.%s: %s", catKey, entryKey, w))
			}
		}
	}
	slices.Sort(warnings)
	return warnings
}

// BuildEnrichedDynamic is the public wrapper for buildEnrichedDynamic.
func BuildEnrichedDynamic(data map[string]map[string]map[string]string) (types.Dynamic, diag.Diagnostics) {
	return buildEnrichedDynamic(data)
}

// ExtractEntries is the public wrapper for extractEntries (used by data source).
func ExtractEntries(val attr.Value) map[string]attr.Value {
	return extractEntries(val)
}

// ResolveTokens gets tokens from the provider config with env var fallback.
// Exported for use by the data source.
func ResolveTokens(cp ConfigProvider) *ProviderTokens {
	return resolveTokens(cp)
}

// ── Schema parsing ──────────────────────────────────────────────────────────

func lookupSchema(categoryKey string, entries map[string]attr.Value, registry map[string]attr.Value) *categorySchema {
	if registry != nil {
		if regEntry, ok := registry[categoryKey]; ok {
			if s := parseSchema(regEntry); s != nil {
				return s
			}
		}
	}
	if inlineSchema, ok := entries["_schema"]; ok {
		return parseSchema(inlineSchema)
	}
	return nil
}

func parseSchema(val attr.Value) *categorySchema {
	attrs := extractAttrs(val)
	if len(attrs) == 0 {
		return nil
	}

	api, _ := getStringAttr(attrs, "api")
	path, _ := getStringAttr(attrs, "path")
	apiVersion, _ := getStringAttr(attrs, "api_version")
	searchFilter, _ := getStringAttr(attrs, "search_filter")

	// token_claims API doesn't need a path (JWT introspection, no HTTP call)
	if api == "" || (path == "" && api != "token_claims") {
		return nil
	}

	schema := &categorySchema{
		API:          api,
		Path:         path,
		APIVersion:   apiVersion,
		SearchFilter: searchFilter,
	}

	if attrVal, ok := attrs["attributes"]; ok && attrVal != nil && !attrVal.IsNull() && !attrVal.IsUnknown() {
		if s, ok := getStringAttr(attrs, "attributes"); ok && s == "*" {
			schema.AllowAny = true
		} else {
			schema.Attributes = extractStringList(attrVal)
		}
	}

	return schema
}

// mergeEntrySchema merges an entry-level _schema override with the category schema.
// If the entry has no _schema, returns the category schema unchanged.
// If there is no category schema, the entry _schema is used as a base (with "arm" as default api).
// Entry _schema fields override category schema fields when non-empty.
func mergeEntrySchema(categorySchema *categorySchema, entryAttrs map[string]attr.Value) *categorySchema {
	entrySchemaVal, ok := entryAttrs["_schema"]
	if !ok || entrySchemaVal == nil || entrySchemaVal.IsNull() || entrySchemaVal.IsUnknown() {
		return categorySchema
	}

	override := parseSchemaPartial(entrySchemaVal)
	if override == nil {
		return categorySchema
	}

	if categorySchema == nil {
		// No category schema — build a minimal one from the entry override.
		// Default to "arm" if api is not specified (most common case).
		if override.API == "" {
			override.API = "arm"
		}
		return override
	}

	// Clone category schema and apply overrides
	merged := *categorySchema
	if override.API != "" {
		merged.API = override.API
	}
	if override.Path != "" {
		merged.Path = override.Path
	}
	if override.APIVersion != "" {
		merged.APIVersion = override.APIVersion
	}
	if override.SearchFilter != "" {
		merged.SearchFilter = override.SearchFilter
	}
	if len(override.Attributes) > 0 {
		merged.Attributes = override.Attributes
	}
	if override.AllowAny {
		merged.AllowAny = true
	}
	return &merged
}

// parseSchemaPartial parses a schema value without requiring api+path (for entry overrides).
func parseSchemaPartial(val attr.Value) *categorySchema {
	attrs := extractAttrs(val)
	if len(attrs) == 0 {
		return nil
	}

	api, _ := getStringAttr(attrs, "api")
	path, _ := getStringAttr(attrs, "path")
	apiVersion, _ := getStringAttr(attrs, "api_version")
	searchFilter, _ := getStringAttr(attrs, "search_filter")

	schema := &categorySchema{
		API:          api,
		Path:         path,
		APIVersion:   apiVersion,
		SearchFilter: searchFilter,
	}

	if attrVal, ok := attrs["attributes"]; ok && attrVal != nil && !attrVal.IsNull() && !attrVal.IsUnknown() {
		if s, ok := getStringAttr(attrs, "attributes"); ok && s == "*" {
			schema.AllowAny = true
		} else {
			schema.Attributes = extractStringList(attrVal)
		}
	}

	return schema
}

// ── _exported_attributes parsing ────────────────────────────────────────────

func parseExportedAttrs(attrs map[string]attr.Value) (exportList []string, isWildcard bool) {
	val, ok := attrs["_exported_attributes"]
	if !ok || val == nil || val.IsNull() || val.IsUnknown() {
		return nil, false
	}

	// Check for plain "*" string
	if s, ok := getStringAttr(attrs, "_exported_attributes"); ok {
		if s == "*" {
			return nil, true
		}
		return []string{s}, false
	}

	// Try as list/tuple
	list := extractStringList(val)
	if len(list) == 1 && list[0] == "*" {
		return nil, true
	}
	return list, false
}

// ── URL building ────────────────────────────────────────────────────────────

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

func substitutePlaceholders(template string, attrs map[string]attr.Value) (string, bool) {
	allResolved := true
	result := placeholderRe.ReplaceAllStringFunc(template, func(match string) string {
		key := match[1 : len(match)-1]
		if val, ok := getStringAttr(attrs, key); ok {
			return val
		}
		allResolved = false
		return match
	})
	return result, allResolved
}

func buildSchemaURL(baseURL string, schema *categorySchema, attrs map[string]attr.Value) string {
	// Shortcut: if entry has an explicit "id" starting with "/", use it directly
	if id, ok := getStringAttr(attrs, "id"); ok && strings.HasPrefix(id, "/") {
		url := baseURL + id
		if schema.APIVersion != "" {
			url += "?api-version=" + schema.APIVersion
		}
		return url
	}

	path, ok := substitutePlaceholders(schema.Path, attrs)
	if !ok {
		return ""
	}

	url := baseURL + path
	if schema.APIVersion != "" {
		url += "?api-version=" + schema.APIVersion
	}

	return url
}

// resolveSearchFilter substitutes placeholders in the search_filter and returns
// the resolved "field eq 'value'" expression for client-side matching.
// Returns "" if the schema has no search_filter or placeholders can't be resolved.
func resolveSearchFilter(schema *categorySchema, attrs map[string]attr.Value) string {
	if schema == nil || schema.SearchFilter == "" {
		return ""
	}
	resolved, ok := substitutePlaceholders(schema.SearchFilter, attrs)
	if !ok {
		return ""
	}
	return resolved
}

// ── Token selection ─────────────────────────────────────────────────────────

func tokenForAPI(tokens *ProviderTokens, api string) string {
	switch api {
	case "arm":
		return tokens.ARMToken
	case "graph":
		return tokens.GraphToken
	case "github":
		return tokens.GitHubToken
	case "maas":
		return tokens.MaasAPIKey
	default:
		return ""
	}
}

// tokenForAPIWithTenant returns the token for a given API, preferring the
// tenant-specific token when a _tenant attribute is present on the entry.
func tokenForAPIWithTenant(tokens *ProviderTokens, api string, attrs map[string]attr.Value) string {
	if api == "arm" {
		if tenantID, ok := getStringAttr(attrs, "_tenant"); ok && tenantID != "" {
			if t, ok := tokens.ARMTenantTokens[tenantID]; ok && t != "" {
				return t
			}
		}
	}
	return tokenForAPI(tokens, api)
}

// ── HTTP helpers ────────────────────────────────────────────────────────────

func doGET(client *http.Client, url, scheme, token string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	setAuthHeader(req, scheme, token)
	req.Header.Set("Accept", "application/json")

	httpResp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.Copy(io.Discard, httpResp.Body)

	switch {
	case httpResp.StatusCode >= 200 && httpResp.StatusCode < 300:
		return nil
	case httpResp.StatusCode == 401 || httpResp.StatusCode == 403:
		return nil // tolerate auth failures for validation
	case httpResp.StatusCode == 404:
		return fmt.Errorf("resource not found (HTTP 404) — verify the resource exists")
	default:
		return fmt.Errorf("unexpected HTTP %d", httpResp.StatusCode)
	}
}

// fetchJSON does a GET and returns the parsed JSON response body.
// Returns (nil, nil) on auth failure (401/403) — caller should skip silently.
func fetchJSON(client *http.Client, url, scheme, token string) (map[string]any, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	setAuthHeader(req, scheme, token)
	req.Header.Set("Accept", "application/json")

	httpResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	switch {
	case httpResp.StatusCode >= 200 && httpResp.StatusCode < 300:
		// success — parse below
	case httpResp.StatusCode == 401 || httpResp.StatusCode == 403:
		return nil, nil // auth failure → skip silently
	case httpResp.StatusCode == 404:
		return nil, fmt.Errorf("resource not found (HTTP 404) — verify the resource exists")
	default:
		return nil, fmt.Errorf("unexpected HTTP %d", httpResp.StatusCode)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON response: %w", err)
	}
	return result, nil
}

func setAuthHeader(req *http.Request, scheme, token string) {
	switch scheme {
	case "token":
		req.Header.Set("Authorization", "token "+token)
	case "oauth1":
		// token is consumer_key:consumer_token:token_secret
		parts := strings.SplitN(token, ":", 3)
		if len(parts) == 3 {
			req.Header.Set("Authorization", oauth1pkg.BuildHeader(parts[0], parts[1], parts[2], oauth1pkg.PLAINTEXT, req.Method, req.URL.String()))
		}
	default:
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// ── JSON response processing ────────────────────────────────────────────────

// decodeJWTClaims decodes the payload of a JWT token (without signature verification,
// since we only need the claims for introspection, not authentication).
// Returns a flat map of claim names → string values for all scalar claims.
// Works with any Azure AD/Entra ID token (user, service principal, managed identity, workload identity).
//
// Key claims mapped to snake_case:
//
//	oid → object_id, tid → tenant_id, upn → user_principal_name,
//	appid → app_id, idtyp → identity_type, name → display_name
func decodeJWTClaims(token string) (map[string]string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT: expected 3 segments, got %d", len(parts))
	}

	// Base64url decode the payload (second segment)
	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decoding JWT payload: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("parsing JWT claims: %w", err)
	}

	// Map well-known short claim names to readable snake_case names
	claimAliases := map[string]string{
		"oid":             "object_id",
		"tid":             "tenant_id",
		"sub":             "subject",
		"upn":             "user_principal_name",
		"name":            "display_name",
		"appid":           "app_id",
		"azp":             "app_id", // v2 tokens use azp instead of appid
		"app_displayname": "app_display_name",
		"idtyp":           "identity_type",
		"iss":             "issuer",
		"aud":             "audience",
		"unique_name":     "unique_name",
	}

	result := make(map[string]string)
	for k, v := range claims {
		// Convert value to string
		var strVal string
		switch val := v.(type) {
		case string:
			strVal = val
		case float64:
			if val == float64(int64(val)) {
				strVal = fmt.Sprintf("%d", int64(val))
			} else {
				strVal = fmt.Sprintf("%g", val)
			}
		case bool:
			strVal = fmt.Sprintf("%t", val)
		default:
			continue // skip arrays, objects
		}

		// Add under the alias name (if known) AND the original name
		if alias, ok := claimAliases[k]; ok {
			if _, exists := result[alias]; !exists {
				result[alias] = strVal
			}
		}
		result[k] = strVal
	}

	// Compute principal_type from idtyp claim:
	//   "user" → "User", "app" → "ServicePrincipal"
	// When idtyp is absent (older v1 tokens), infer from other claims:
	//   upn present → User, appid present without upn → ServicePrincipal
	if _, exists := result["principal_type"]; !exists {
		switch result["idtyp"] {
		case "user":
			result["principal_type"] = "User"
		case "app":
			result["principal_type"] = "ServicePrincipal"
		default:
			if _, hasUPN := result["upn"]; hasUPN {
				result["principal_type"] = "User"
			} else if _, hasAppID := result["appid"]; hasAppID {
				result["principal_type"] = "ServicePrincipal"
			}
		}
	}

	return result, nil
}

// extractFirstResult handles list responses ({"value": [...]}) with optional client-side filtering.
// When searchFilter is non-empty (e.g. "tenantId eq '...'"), it filters the array client-side
// rather than relying on the API's $filter support.  For direct GET responses, returns the object as-is.
// Returns nil if no matching result is found.
func extractFirstResult(jsonBody map[string]any, searchFilter string) map[string]any {
	if value, ok := jsonBody["value"]; ok {
		if arr, ok := value.([]any); ok {
			if len(arr) == 0 {
				return nil
			}
			// Client-side filter: parse "field eq 'value'" and match
			if searchFilter != "" {
				field, val, ok := parseEqFilter(searchFilter)
				if ok {
					for _, item := range arr {
						if obj, ok := item.(map[string]any); ok {
							if fmt.Sprintf("%v", walkJSONPath(obj, field)) == val {
								return obj
							}
						}
					}
					return nil // no match
				}
			}
			// No filter or unparseable — return first item
			if first, ok := arr[0].(map[string]any); ok {
				return first
			}
			return nil
		}
	}
	return jsonBody
}

// parseEqFilter parses a simple OData filter like "field eq 'value'" into (field, value, ok).
func parseEqFilter(filter string) (string, string, bool) {
	parts := strings.SplitN(filter, " eq ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	field := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	// Strip surrounding quotes
	val = strings.Trim(val, "'\"")
	return field, val, true
}

// walkJSONPath walks a dotted path (e.g. "properties.roleName") into a nested
// JSON object. Returns nil if any segment is missing.
func walkJSONPath(obj map[string]any, path string) any {
	segments := strings.Split(path, ".")
	var current any = obj
	for _, seg := range segments {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return current
}

// flattenJSONResponse converts a JSON object to a flat map[string]string with snake_case keys.
// Handles top-level scalars and flattens the ARM "properties" sub-object.
// Two-pass: top-level scalars first, then properties promotion (never overwrites).
func flattenJSONResponse(obj map[string]any) map[string]string {
	result := make(map[string]string)

	// Pass 1: collect all top-level scalar fields.
	for k, v := range obj {
		addScalar(result, camelToSnake(k), v)
	}

	// Pass 2: promote properties sub-keys, skipping any that collide
	// with an existing top-level key (e.g. "type").
	if props, ok := obj["properties"].(map[string]any); ok {
		for pk, pv := range props {
			propKey := camelToSnake(pk)
			if _, exists := result[propKey]; !exists {
				addScalar(result, propKey, pv)
			}
		}
	}

	return result
}

func addScalar(m map[string]string, key string, v any) {
	switch val := v.(type) {
	case string:
		m[key] = val
	case float64:
		if val == float64(int64(val)) {
			m[key] = fmt.Sprintf("%d", int64(val))
		} else {
			m[key] = fmt.Sprintf("%g", val)
		}
	case bool:
		m[key] = fmt.Sprintf("%t", val)
	}
}

// camelToSnake converts camelCase to snake_case.
func camelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

// ── Output reconstruction ───────────────────────────────────────────────────

// buildEnrichedDynamic builds a types.Dynamic from the enriched string maps.
func buildEnrichedDynamic(data map[string]map[string]map[string]string) (types.Dynamic, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	categoryTypes := make(map[string]attr.Type)
	categoryValues := make(map[string]attr.Value)

	for _, catKey := range sortedKeys(data) {
		entries := data[catKey]

		entryTypes := make(map[string]attr.Type)
		entryValues := make(map[string]attr.Value)

		for _, entryKey := range sortedKeys(entries) {
			attrs := entries[entryKey]

			attrTypes := make(map[string]attr.Type)
			attrValues := make(map[string]attr.Value)

			for _, ak := range sortedKeys(attrs) {
				attrTypes[ak] = types.StringType
				attrValues[ak] = types.StringValue(attrs[ak])
			}

			entryObj, d := types.ObjectValue(attrTypes, attrValues)
			allDiags.Append(d...)
			if d.HasError() {
				return types.DynamicNull(), allDiags
			}

			entryTypes[entryKey] = entryObj.Type(context.Background())
			entryValues[entryKey] = entryObj
		}

		catObj, d := types.ObjectValue(entryTypes, entryValues)
		allDiags.Append(d...)
		if d.HasError() {
			return types.DynamicNull(), allDiags
		}

		categoryTypes[catKey] = catObj.Type(context.Background())
		categoryValues[catKey] = catObj
	}

	rootObj, d := types.ObjectValue(categoryTypes, categoryValues)
	allDiags.Append(d...)
	if d.HasError() {
		return types.DynamicNull(), allDiags
	}

	return types.DynamicValue(rootObj), allDiags
}

// sortedKeys returns map keys in sorted order (generic helper).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ── ref:existing. resolution ────────────────────────────────────────────────

// resolveExistingRefsInAttrs mutates an attrs map in place, replacing any
// string value that starts with "ref:existing." by walking the enrichedData
// cache of already-processed categories. Unresolvable refs are left as-is.
func resolveExistingRefsInAttrs(attrs map[string]attr.Value, enrichedData map[string]map[string]map[string]string) {
	const prefix = "ref:existing."
	for k, v := range attrs {
		s, ok := getStringAttr(map[string]attr.Value{k: v}, k)
		if !ok || !strings.HasPrefix(s, prefix) {
			continue
		}
		path := strings.TrimPrefix(s, prefix)
		segments := strings.SplitN(path, ".", 3) // category.entry.attr
		if len(segments) != 3 {
			continue
		}
		category, entry, attrKey := segments[0], segments[1], segments[2]
		if entries, ok := enrichedData[category]; ok {
			if entryAttrs, ok := entries[entry]; ok {
				if resolved, ok := entryAttrs[attrKey]; ok {
					attrs[k] = types.StringValue(resolved)
				}
			}
		}
	}
}

// ── Placeholder helpers ─────────────────────────────────────────────────────

func extractPlaceholders(templates ...string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, t := range templates {
		for _, match := range placeholderRe.FindAllStringSubmatch(t, -1) {
			if len(match) > 1 && !seen[match[1]] {
				seen[match[1]] = true
				result = append(result, match[1])
			}
		}
	}
	return result
}

// ── Attribute helpers ───────────────────────────────────────────────────────

func extractStringList(val attr.Value) []string {
	switch v := val.(type) {
	case types.List:
		var result []string
		for _, elem := range v.Elements() {
			if s, ok := elem.(types.String); ok {
				result = append(result, s.ValueString())
			}
		}
		return result
	case types.Tuple:
		var result []string
		for _, elem := range v.Elements() {
			if s, ok := elem.(types.String); ok {
				result = append(result, s.ValueString())
			}
		}
		return result
	case types.Dynamic:
		return extractStringList(v.UnderlyingValue())
	}
	return nil
}

func getStringAttr(attrs map[string]attr.Value, key string) (string, bool) {
	v, ok := attrs[key]
	if !ok || v == nil || v.IsNull() || v.IsUnknown() {
		return "", false
	}
	switch s := v.(type) {
	case types.String:
		return s.ValueString(), true
	case types.Dynamic:
		if str, ok := s.UnderlyingValue().(types.String); ok {
			return str.ValueString(), true
		}
	}
	return "", false
}
