package functions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ── camelToSnake ────────────────────────────────────────────────────────────

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"camelCase", "camel_case"},
		{"PascalCase", "pascal_case"},
		{"simple", "simple"},
		{"tenantId", "tenant_id"},
		{"subscriptionId", "subscription_id"},
		{"resourceGroupName", "resource_group_name"},
		{"", ""},
		{"ID", "i_d"},
		{"displayName", "display_name"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := camelToSnake(tc.in)
			if got != tc.want {
				t.Errorf("camelToSnake(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ── addScalar ───────────────────────────────────────────────────────────────

func TestAddScalar(t *testing.T) {
	m := make(map[string]string)
	addScalar(m, "str", "hello")
	addScalar(m, "int", float64(42))
	addScalar(m, "float", 3.14)
	addScalar(m, "bool_true", true)
	addScalar(m, "bool_false", false)
	addScalar(m, "null", nil)             // should be ignored
	addScalar(m, "arr", []interface{}{1}) // should be ignored

	expect := map[string]string{
		"str":        "hello",
		"int":        "42",
		"float":      "3.14",
		"bool_true":  "true",
		"bool_false": "false",
	}
	for k, want := range expect {
		if got, ok := m[k]; !ok || got != want {
			t.Errorf("m[%q] = %q, want %q", k, got, want)
		}
	}
	if _, ok := m["null"]; ok {
		t.Error("nil value should not be added")
	}
	if _, ok := m["arr"]; ok {
		t.Error("array value should not be added")
	}
}

// ── parseEqFilter ───────────────────────────────────────────────────────────

func TestParseEqFilter(t *testing.T) {
	tests := []struct {
		input    string
		field    string
		value    string
		expectOK bool
	}{
		{"tenantId eq 'abc-123'", "tenantId", "abc-123", true},
		{`displayName eq "My App"`, "displayName", "My App", true},
		{"no-eq-here", "", "", false},
		{"field eq 'value with spaces'", "field", "value with spaces", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			field, val, ok := parseEqFilter(tc.input)
			if ok != tc.expectOK {
				t.Fatalf("ok = %v, want %v", ok, tc.expectOK)
			}
			if ok {
				if field != tc.field {
					t.Errorf("field = %q, want %q", field, tc.field)
				}
				if val != tc.value {
					t.Errorf("value = %q, want %q", val, tc.value)
				}
			}
		})
	}
}

// ── walkJSONPath ────────────────────────────────────────────────────────────

func TestWalkJSONPath(t *testing.T) {
	obj := map[string]interface{}{
		"name": "rg-test",
		"properties": map[string]interface{}{
			"provisioningState": "Succeeded",
			"nested": map[string]interface{}{
				"deep": "value",
			},
		},
	}

	tests := []struct {
		path string
		want interface{}
	}{
		{"name", "rg-test"},
		{"properties.provisioningState", "Succeeded"},
		{"properties.nested.deep", "value"},
		{"missing", nil},
		{"properties.missing", nil},
		{"name.invalid", nil}, // can't descend into a string
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := walkJSONPath(obj, tc.path)
			if got != tc.want {
				t.Errorf("walkJSONPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// ── flattenJSONResponse ─────────────────────────────────────────────────────

func TestFlattenJSONResponse_Simple(t *testing.T) {
	obj := map[string]interface{}{
		"name":     "rg-test",
		"location": "westeurope",
		"type":     "Microsoft.Resources/resourceGroups",
	}
	got := flattenJSONResponse(obj)
	if got["name"] != "rg-test" {
		t.Errorf("name = %q", got["name"])
	}
	if got["location"] != "westeurope" {
		t.Errorf("location = %q", got["location"])
	}
}

func TestFlattenJSONResponse_WithProperties(t *testing.T) {
	obj := map[string]interface{}{
		"name": "rg-test",
		"type": "Microsoft.Resources/resourceGroups",
		"properties": map[string]interface{}{
			"provisioningState": "Succeeded",
			"type":              "should-not-overwrite",
		},
	}
	got := flattenJSONResponse(obj)
	// provisioningState should be promoted as provisioning_state
	if got["provisioning_state"] != "Succeeded" {
		t.Errorf("provisioning_state = %q, want %q", got["provisioning_state"], "Succeeded")
	}
	// top-level "type" should NOT be overwritten by properties.type
	if got["type"] != "Microsoft.Resources/resourceGroups" {
		t.Errorf("type should be top-level value, got %q", got["type"])
	}
}

func TestFlattenJSONResponse_IgnoresNestedObjects(t *testing.T) {
	obj := map[string]interface{}{
		"name":    "test",
		"tags":    map[string]interface{}{"env": "dev"}, // not a scalar
		"enabled": true,
		"count":   float64(5),
	}
	got := flattenJSONResponse(obj)
	if got["name"] != "test" {
		t.Errorf("name = %q", got["name"])
	}
	if got["enabled"] != "true" {
		t.Errorf("enabled = %q", got["enabled"])
	}
	if got["count"] != "5" {
		t.Errorf("count = %q", got["count"])
	}
	if _, ok := got["tags"]; ok {
		t.Error("tags (nested object) should not be in flattened output")
	}
}

// ── extractFirstResult ──────────────────────────────────────────────────────

func TestExtractFirstResult_DirectObject(t *testing.T) {
	obj := map[string]interface{}{"name": "rg-test"}
	got := extractFirstResult(obj, "")
	if got["name"] != "rg-test" {
		t.Errorf("expected direct object passthrough")
	}
}

func TestExtractFirstResult_ListNoFilter(t *testing.T) {
	obj := map[string]interface{}{
		"value": []interface{}{
			map[string]interface{}{"name": "first"},
			map[string]interface{}{"name": "second"},
		},
	}
	got := extractFirstResult(obj, "")
	if got["name"] != "first" {
		t.Errorf("expected first item, got %v", got)
	}
}

func TestExtractFirstResult_ListWithFilter(t *testing.T) {
	obj := map[string]interface{}{
		"value": []interface{}{
			map[string]interface{}{"tenantId": "aaa", "name": "first"},
			map[string]interface{}{"tenantId": "bbb", "name": "second"},
		},
	}
	got := extractFirstResult(obj, "tenantId eq 'bbb'")
	if got == nil {
		t.Fatal("expected match")
	}
	if got["name"] != "second" {
		t.Errorf("expected second item, got %v", got)
	}
}

func TestExtractFirstResult_ListWithFilterNoMatch(t *testing.T) {
	obj := map[string]interface{}{
		"value": []interface{}{
			map[string]interface{}{"tenantId": "aaa"},
		},
	}
	got := extractFirstResult(obj, "tenantId eq 'zzz'")
	if got != nil {
		t.Errorf("expected nil for no match, got %v", got)
	}
}

func TestExtractFirstResult_EmptyList(t *testing.T) {
	obj := map[string]interface{}{
		"value": []interface{}{},
	}
	got := extractFirstResult(obj, "")
	if got != nil {
		t.Errorf("expected nil for empty list, got %v", got)
	}
}

// ── extractPlaceholders ─────────────────────────────────────────────────────

func TestExtractPlaceholders(t *testing.T) {
	result := extractPlaceholders(
		"/subscriptions/{subscription_id}/resourcegroups/{name}",
		"displayName eq '{display_name}'",
	)
	expected := map[string]bool{
		"subscription_id": true,
		"name":            true,
		"display_name":    true,
	}
	if len(result) != len(expected) {
		t.Fatalf("got %d placeholders, want %d: %v", len(result), len(expected), result)
	}
	for _, p := range result {
		if !expected[p] {
			t.Errorf("unexpected placeholder %q", p)
		}
	}
}

func TestExtractPlaceholders_Deduplication(t *testing.T) {
	result := extractPlaceholders("{name}/child/{name}")
	if len(result) != 1 {
		t.Errorf("expected 1 unique placeholder, got %d: %v", len(result), result)
	}
}

// ── substitutePlaceholders ──────────────────────────────────────────────────

func TestSubstitutePlaceholders_AllResolved(t *testing.T) {
	attrs := map[string]attr.Value{
		"subscription_id": types.StringValue("sub-123"),
		"name":            types.StringValue("rg-test"),
	}
	result, ok := substitutePlaceholders("/subscriptions/{subscription_id}/resourcegroups/{name}", attrs)
	if !ok {
		t.Fatal("expected all resolved")
	}
	if result != "/subscriptions/sub-123/resourcegroups/rg-test" {
		t.Errorf("got %q", result)
	}
}

func TestSubstitutePlaceholders_Missing(t *testing.T) {
	attrs := map[string]attr.Value{
		"subscription_id": types.StringValue("sub-123"),
	}
	_, ok := substitutePlaceholders("/subscriptions/{subscription_id}/resourcegroups/{name}", attrs)
	if ok {
		t.Fatal("expected not all resolved")
	}
}

// ── buildSchemaURL ──────────────────────────────────────────────────────────

func TestBuildSchemaURL_WithPath(t *testing.T) {
	schema := &categorySchema{
		Path:       "/subscriptions/{subscription_id}/resourcegroups/{name}",
		APIVersion: "2022-09-01",
	}
	attrs := map[string]attr.Value{
		"subscription_id": types.StringValue("sub-123"),
		"name":            types.StringValue("rg-test"),
	}
	url := buildSchemaURL("https://management.azure.com", schema, attrs)
	want := "https://management.azure.com/subscriptions/sub-123/resourcegroups/rg-test?api-version=2022-09-01"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildSchemaURL_WithID(t *testing.T) {
	schema := &categorySchema{
		Path:       "/subscriptions/{subscription_id}/resourcegroups/{name}",
		APIVersion: "2022-09-01",
	}
	attrs := map[string]attr.Value{
		"id": types.StringValue("/subscriptions/sub-123/resourcegroups/rg-test"),
	}
	url := buildSchemaURL("https://management.azure.com", schema, attrs)
	want := "https://management.azure.com/subscriptions/sub-123/resourcegroups/rg-test?api-version=2022-09-01"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildSchemaURL_WithIDNoAPIVersion(t *testing.T) {
	schema := &categorySchema{
		Path: "/some/path/{name}",
	}
	attrs := map[string]attr.Value{
		"id": types.StringValue("/direct/path/here"),
	}
	url := buildSchemaURL("https://api.example.com", schema, attrs)
	want := "https://api.example.com/direct/path/here"
	if url != want {
		t.Errorf("got %q, want %q", url, want)
	}
}

func TestBuildSchemaURL_MissingPlaceholder(t *testing.T) {
	schema := &categorySchema{
		Path: "/subscriptions/{subscription_id}/resourcegroups/{name}",
	}
	attrs := map[string]attr.Value{
		"subscription_id": types.StringValue("sub-123"),
		// name is missing
	}
	url := buildSchemaURL("https://management.azure.com", schema, attrs)
	if url != "" {
		t.Errorf("expected empty URL for unresolvable placeholders, got %q", url)
	}
}

// ── resolveSearchFilter ─────────────────────────────────────────────────────

func TestResolveSearchFilter(t *testing.T) {
	schema := &categorySchema{
		SearchFilter: "displayName eq '{display_name}'",
	}
	attrs := map[string]attr.Value{
		"display_name": types.StringValue("My App"),
	}
	got := resolveSearchFilter(schema, attrs)
	if got != "displayName eq 'My App'" {
		t.Errorf("got %q", got)
	}
}

func TestResolveSearchFilter_NilSchema(t *testing.T) {
	got := resolveSearchFilter(nil, nil)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestResolveSearchFilter_MissingPlaceholder(t *testing.T) {
	schema := &categorySchema{
		SearchFilter: "field eq '{missing}'",
	}
	got := resolveSearchFilter(schema, map[string]attr.Value{})
	if got != "" {
		t.Errorf("expected empty for missing placeholder, got %q", got)
	}
}

// ── tokenForAPI ─────────────────────────────────────────────────────────────

func TestTokenForAPI(t *testing.T) {
	tokens := &ProviderTokens{
		ARMToken:    "arm-tok",
		GraphToken:  "graph-tok",
		GitHubToken: "gh-tok",
	}
	if got := tokenForAPI(tokens, "arm"); got != "arm-tok" {
		t.Errorf("arm = %q", got)
	}
	if got := tokenForAPI(tokens, "graph"); got != "graph-tok" {
		t.Errorf("graph = %q", got)
	}
	if got := tokenForAPI(tokens, "github"); got != "gh-tok" {
		t.Errorf("github = %q", got)
	}
	if got := tokenForAPI(tokens, "unknown"); got != "" {
		t.Errorf("unknown = %q", got)
	}
}

// ── tokenForAPIWithTenant ───────────────────────────────────────────────────

func TestTokenForAPIWithTenant_UseTenantToken(t *testing.T) {
	tokens := &ProviderTokens{
		ARMToken:        "default-arm",
		ARMTenantTokens: map[string]string{"tenant-1": "tenant-1-token"},
	}
	attrs := map[string]attr.Value{
		"_tenant": types.StringValue("tenant-1"),
	}
	got := tokenForAPIWithTenant(tokens, "arm", attrs)
	if got != "tenant-1-token" {
		t.Errorf("expected tenant-specific token, got %q", got)
	}
}

func TestTokenForAPIWithTenant_FallbackToDefault(t *testing.T) {
	tokens := &ProviderTokens{
		ARMToken:        "default-arm",
		ARMTenantTokens: map[string]string{"tenant-1": "tenant-1-token"},
	}
	attrs := map[string]attr.Value{
		"_tenant": types.StringValue("tenant-unknown"),
	}
	got := tokenForAPIWithTenant(tokens, "arm", attrs)
	if got != "default-arm" {
		t.Errorf("expected default token, got %q", got)
	}
}

func TestTokenForAPIWithTenant_NonARM(t *testing.T) {
	tokens := &ProviderTokens{
		GraphToken:      "graph-tok",
		ARMTenantTokens: map[string]string{"t1": "t1-tok"},
	}
	attrs := map[string]attr.Value{
		"_tenant": types.StringValue("t1"),
	}
	// _tenant is only used for ARM
	got := tokenForAPIWithTenant(tokens, "graph", attrs)
	if got != "graph-tok" {
		t.Errorf("expected graph token, got %q", got)
	}
}

// ── setAuthHeader ───────────────────────────────────────────────────────────

func TestSetAuthHeader_Bearer(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	setAuthHeader(req, "Bearer", "my-token")
	if got := req.Header.Get("Authorization"); got != "Bearer my-token" {
		t.Errorf("got %q", got)
	}
}

func TestSetAuthHeader_Token(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	setAuthHeader(req, "token", "gh-token")
	if got := req.Header.Get("Authorization"); got != "token gh-token" {
		t.Errorf("got %q", got)
	}
}

// ── sortedKeys ──────────────────────────────────────────────────────────────

func TestSortedKeys(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	got := sortedKeys(m)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestSortedKeys_Empty(t *testing.T) {
	got := sortedKeys(map[string]string{})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ── getStringAttr ───────────────────────────────────────────────────────────

func TestGetStringAttr_String(t *testing.T) {
	attrs := map[string]attr.Value{"key": types.StringValue("val")}
	got, ok := getStringAttr(attrs, "key")
	if !ok || got != "val" {
		t.Errorf("got %q, %v", got, ok)
	}
}

func TestGetStringAttr_Dynamic(t *testing.T) {
	attrs := map[string]attr.Value{
		"key": types.DynamicValue(types.StringValue("dyn-val")),
	}
	got, ok := getStringAttr(attrs, "key")
	if !ok || got != "dyn-val" {
		t.Errorf("got %q, %v", got, ok)
	}
}

func TestGetStringAttr_Missing(t *testing.T) {
	attrs := map[string]attr.Value{}
	_, ok := getStringAttr(attrs, "key")
	if ok {
		t.Error("expected not ok for missing key")
	}
}

func TestGetStringAttr_Null(t *testing.T) {
	attrs := map[string]attr.Value{"key": types.StringNull()}
	_, ok := getStringAttr(attrs, "key")
	if ok {
		t.Error("expected not ok for null value")
	}
}

// ── extractStringList ───────────────────────────────────────────────────────

func TestExtractStringList_Tuple(t *testing.T) {
	tuple, diags := types.TupleValue(
		[]attr.Type{types.StringType, types.StringType},
		[]attr.Value{types.StringValue("a"), types.StringValue("b")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractStringList(tuple)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

func TestExtractStringList_List(t *testing.T) {
	list, diags := types.ListValue(types.StringType, []attr.Value{
		types.StringValue("x"), types.StringValue("y"),
	})
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractStringList(list)
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("got %v", got)
	}
}

func TestExtractStringList_Dynamic(t *testing.T) {
	tuple, diags := types.TupleValue(
		[]attr.Type{types.StringType},
		[]attr.Value{types.StringValue("z")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	dyn := types.DynamicValue(tuple)
	got := extractStringList(dyn)
	if len(got) != 1 || got[0] != "z" {
		t.Errorf("got %v", got)
	}
}

// ── parseSchema ─────────────────────────────────────────────────────────────

func TestParseSchema_Full(t *testing.T) {
	schemaObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"api":           types.StringType,
			"path":          types.StringType,
			"api_version":   types.StringType,
			"search_filter": types.StringType,
		},
		map[string]attr.Value{
			"api":           types.StringValue("arm"),
			"path":          types.StringValue("/subscriptions/{subscription_id}"),
			"api_version":   types.StringValue("2022-09-01"),
			"search_filter": types.StringValue(""),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	s := parseSchema(schemaObj)
	if s == nil {
		t.Fatal("expected non-nil schema")
	}
	if s.API != "arm" {
		t.Errorf("api = %q", s.API)
	}
	if s.Path != "/subscriptions/{subscription_id}" {
		t.Errorf("path = %q", s.Path)
	}
	if s.APIVersion != "2022-09-01" {
		t.Errorf("api_version = %q", s.APIVersion)
	}
}

func TestParseSchema_TokenClaims(t *testing.T) {
	schemaObj, diags := types.ObjectValue(
		map[string]attr.Type{"api": types.StringType},
		map[string]attr.Value{"api": types.StringValue("token_claims")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	s := parseSchema(schemaObj)
	if s == nil {
		t.Fatal("expected non-nil for token_claims (no path needed)")
	}
	if s.API != "token_claims" {
		t.Errorf("api = %q", s.API)
	}
}

func TestParseSchema_NoAPINoPath(t *testing.T) {
	schemaObj, diags := types.ObjectValue(
		map[string]attr.Type{"something": types.StringType},
		map[string]attr.Value{"something": types.StringValue("else")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	s := parseSchema(schemaObj)
	if s != nil {
		t.Error("expected nil schema when api and path are absent")
	}
}

func TestParseSchema_AllowAny(t *testing.T) {
	schemaObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"api":        types.StringType,
			"path":       types.StringType,
			"attributes": types.StringType,
		},
		map[string]attr.Value{
			"api":        types.StringValue("arm"),
			"path":       types.StringValue("/test/{name}"),
			"attributes": types.StringValue("*"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	s := parseSchema(schemaObj)
	if s == nil {
		t.Fatal("expected non-nil")
	}
	if !s.AllowAny {
		t.Error("expected AllowAny=true")
	}
}

// ── mergeEntrySchema ────────────────────────────────────────────────────────

func TestMergeEntrySchema_NoOverride(t *testing.T) {
	base := &categorySchema{API: "arm", Path: "/test/{name}", APIVersion: "2022-01-01"}
	result := mergeEntrySchema(base, map[string]attr.Value{})
	if result != base {
		t.Error("expected same pointer when no _schema override")
	}
}

func TestMergeEntrySchema_OverridePath(t *testing.T) {
	base := &categorySchema{API: "arm", Path: "/original/{name}", APIVersion: "2022-01-01"}
	overrideObj, diags := types.ObjectValue(
		map[string]attr.Type{"path": types.StringType, "api_version": types.StringType},
		map[string]attr.Value{
			"path":        types.StringValue("/override/{name}"),
			"api_version": types.StringValue("2023-01-01"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	attrs := map[string]attr.Value{"_schema": overrideObj}

	result := mergeEntrySchema(base, attrs)
	if result.Path != "/override/{name}" {
		t.Errorf("path = %q, want /override/{name}", result.Path)
	}
	if result.APIVersion != "2023-01-01" {
		t.Errorf("api_version = %q", result.APIVersion)
	}
	if result.API != "arm" {
		t.Errorf("api should be inherited, got %q", result.API)
	}
}

func TestMergeEntrySchema_NoCategorySchema(t *testing.T) {
	overrideObj, diags := types.ObjectValue(
		map[string]attr.Type{"path": types.StringType},
		map[string]attr.Value{"path": types.StringValue("/standalone/{name}")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	attrs := map[string]attr.Value{"_schema": overrideObj}

	result := mergeEntrySchema(nil, attrs)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.API != "arm" {
		t.Errorf("api should default to 'arm', got %q", result.API)
	}
	if result.Path != "/standalone/{name}" {
		t.Errorf("path = %q", result.Path)
	}
}

// ── parseExportedAttrs ──────────────────────────────────────────────────────

func TestParseExportedAttrs_Wildcard(t *testing.T) {
	attrs := map[string]attr.Value{
		"_exported_attributes": types.StringValue("*"),
	}
	list, isWild := parseExportedAttrs(attrs)
	if !isWild {
		t.Error("expected wildcard")
	}
	if list != nil {
		t.Errorf("expected nil list for wildcard, got %v", list)
	}
}

func TestParseExportedAttrs_SingleString(t *testing.T) {
	attrs := map[string]attr.Value{
		"_exported_attributes": types.StringValue("location"),
	}
	list, isWild := parseExportedAttrs(attrs)
	if isWild {
		t.Error("not wildcard")
	}
	if len(list) != 1 || list[0] != "location" {
		t.Errorf("got %v", list)
	}
}

func TestParseExportedAttrs_Missing(t *testing.T) {
	attrs := map[string]attr.Value{}
	list, isWild := parseExportedAttrs(attrs)
	if isWild || list != nil {
		t.Error("expected nil/false for missing attribute")
	}
}

// ── lookupSchema ────────────────────────────────────────────────────────────

func TestLookupSchema_FromRegistry(t *testing.T) {
	regSchemaObj, diags := types.ObjectValue(
		map[string]attr.Type{"api": types.StringType, "path": types.StringType},
		map[string]attr.Value{
			"api":  types.StringValue("arm"),
			"path": types.StringValue("/from-registry/{name}"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	registry := map[string]attr.Value{
		"my_category": regSchemaObj,
	}
	entries := map[string]attr.Value{}
	s := lookupSchema("my_category", entries, registry)
	if s == nil || s.Path != "/from-registry/{name}" {
		t.Errorf("expected registry schema, got %v", s)
	}
}

func TestLookupSchema_FromInline(t *testing.T) {
	inlineSchemaObj, diags := types.ObjectValue(
		map[string]attr.Type{"api": types.StringType, "path": types.StringType},
		map[string]attr.Value{
			"api":  types.StringValue("github"),
			"path": types.StringValue("/repos/{org}/{name}"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	entries := map[string]attr.Value{
		"_schema": inlineSchemaObj,
	}
	s := lookupSchema("my_category", entries, nil)
	if s == nil || s.API != "github" {
		t.Errorf("expected inline schema, got %v", s)
	}
}

func TestLookupSchema_None(t *testing.T) {
	s := lookupSchema("unknown", map[string]attr.Value{}, nil)
	if s != nil {
		t.Error("expected nil when no schema found")
	}
}

// ── resolveExistingRefsInAttrs ──────────────────────────────────────────────

func TestResolveExistingRefsInAttrs(t *testing.T) {
	enrichedData := map[string]map[string]map[string]string{
		"resource_groups": {
			"rg1": {"location": "westeurope", "name": "rg-test"},
		},
	}
	attrs := map[string]attr.Value{
		"location": types.StringValue("ref:existing.resource_groups.rg1.location"),
		"static":   types.StringValue("unchanged"),
	}

	resolveExistingRefsInAttrs(attrs, enrichedData)

	if s, ok := getStringAttr(attrs, "location"); !ok || s != "westeurope" {
		t.Errorf("location = %q, want westeurope", s)
	}
	if s, ok := getStringAttr(attrs, "static"); !ok || s != "unchanged" {
		t.Errorf("static = %q, want unchanged", s)
	}
}

func TestResolveExistingRefsInAttrs_Unresolvable(t *testing.T) {
	attrs := map[string]attr.Value{
		"val": types.StringValue("ref:existing.missing.key.attr"),
	}
	resolveExistingRefsInAttrs(attrs, map[string]map[string]map[string]string{})
	// Should remain unchanged
	if s, ok := getStringAttr(attrs, "val"); !ok || s != "ref:existing.missing.key.attr" {
		t.Errorf("unresolvable ref should stay unchanged, got %q", s)
	}
}

// ── decodeJWTClaims ─────────────────────────────────────────────────────────

func makeTestJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + payloadB64 + ".fake-signature"
}

func TestDecodeJWTClaims_Basic(t *testing.T) {
	token := makeTestJWT(map[string]interface{}{
		"oid":   "obj-123",
		"tid":   "tenant-456",
		"upn":   "user@example.com",
		"name":  "Test User",
		"idtyp": "user",
	})

	claims, err := decodeJWTClaims(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims["object_id"] != "obj-123" {
		t.Errorf("object_id = %q", claims["object_id"])
	}
	if claims["tenant_id"] != "tenant-456" {
		t.Errorf("tenant_id = %q", claims["tenant_id"])
	}
	if claims["user_principal_name"] != "user@example.com" {
		t.Errorf("upn = %q", claims["user_principal_name"])
	}
	if claims["principal_type"] != "User" {
		t.Errorf("principal_type = %q", claims["principal_type"])
	}
}

func TestDecodeJWTClaims_ServicePrincipal(t *testing.T) {
	token := makeTestJWT(map[string]interface{}{
		"oid":   "sp-obj",
		"appid": "app-123",
		"idtyp": "app",
	})

	claims, err := decodeJWTClaims(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims["principal_type"] != "ServicePrincipal" {
		t.Errorf("principal_type = %q", claims["principal_type"])
	}
	if claims["app_id"] != "app-123" {
		t.Errorf("app_id = %q", claims["app_id"])
	}
}

func TestDecodeJWTClaims_InvalidJWT(t *testing.T) {
	_, err := decodeJWTClaims("not-a-jwt")
	if err == nil {
		t.Error("expected error for invalid JWT")
	}
}

func TestDecodeJWTClaims_NumericClaim(t *testing.T) {
	token := makeTestJWT(map[string]interface{}{
		"oid": "obj-1",
		"iat": float64(1700000000),
	})
	claims, err := decodeJWTClaims(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims["iat"] != "1700000000" {
		t.Errorf("iat = %q", claims["iat"])
	}
}

// ── doGET ───────────────────────────────────────────────────────────────────

func TestDoGET_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := srv.Client()
	err := doGET(client, srv.URL+"/test", "Bearer", "tok")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDoGET_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client := srv.Client()
	err := doGET(client, srv.URL+"/test", "Bearer", "tok")
	if err == nil {
		t.Error("expected error for 404")
	}
}

func TestDoGET_AuthFailureTolerated(t *testing.T) {
	for _, code := range []int{401, 403} {
		t.Run(fmt.Sprintf("HTTP_%d", code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			client := srv.Client()
			err := doGET(client, srv.URL+"/test", "Bearer", "tok")
			if err != nil {
				t.Errorf("expected auth failure to be tolerated, got %v", err)
			}
		})
	}
}

func TestDoGET_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := srv.Client()
	err := doGET(client, srv.URL+"/test", "Bearer", "tok")
	if err == nil {
		t.Error("expected error for 500")
	}
}

// ── fetchJSON ───────────────────────────────────────────────────────────────

func TestFetchJSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"test","location":"westeurope"}`)
	}))
	defer srv.Close()

	client := srv.Client()
	body, err := fetchJSON(client, srv.URL+"/test", "Bearer", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if body["name"] != "test" {
		t.Errorf("name = %v", body["name"])
	}
}

func TestFetchJSON_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client := srv.Client()
	_, err := fetchJSON(client, srv.URL+"/test", "Bearer", "tok")
	if err == nil {
		t.Error("expected error for 404")
	}
}

func TestFetchJSON_AuthReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()

	client := srv.Client()
	body, err := fetchJSON(client, srv.URL+"/test", "Bearer", "tok")
	if err != nil {
		t.Errorf("expected nil error for auth failure, got %v", err)
	}
	if body != nil {
		t.Errorf("expected nil body for auth failure, got %v", body)
	}
}

// ── CollectWarnings ─────────────────────────────────────────────────────────

func TestCollectWarnings(t *testing.T) {
	data := map[string]map[string]map[string]string{
		"cat_b": {
			"entry1": {"_warning": "problem B"},
		},
		"cat_a": {
			"entry1": {"name": "ok"},
			"entry2": {"_warning": "problem A"},
		},
	}
	warnings := CollectWarnings(data)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
	// Should be sorted
	if warnings[0] != "externals.cat_a.entry2: problem A" {
		t.Errorf("warnings[0] = %q", warnings[0])
	}
	if warnings[1] != "externals.cat_b.entry1: problem B" {
		t.Errorf("warnings[1] = %q", warnings[1])
	}
}

func TestCollectWarnings_None(t *testing.T) {
	data := map[string]map[string]map[string]string{
		"cat": {"entry": {"name": "ok"}},
	}
	warnings := CollectWarnings(data)
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %v", warnings)
	}
}

// ── buildEnrichedDynamic ────────────────────────────────────────────────────

func TestBuildEnrichedDynamic(t *testing.T) {
	data := map[string]map[string]map[string]string{
		"resource_groups": {
			"rg1": {"name": "rg-test", "location": "westeurope"},
		},
	}
	result, diags := BuildEnrichedDynamic(data)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %s", diags.Errors())
	}
	if result.IsNull() {
		t.Fatal("expected non-null result")
	}
	// Verify it's a valid Dynamic wrapping an object
	underlying := result.UnderlyingValue()
	if underlying == nil {
		t.Fatal("underlying is nil")
	}
	obj, ok := underlying.(types.Object)
	if !ok {
		t.Fatalf("expected Object, got %T", underlying)
	}
	if _, ok := obj.Attributes()["resource_groups"]; !ok {
		t.Error("expected resource_groups key in result")
	}
}

func TestBuildEnrichedDynamic_Empty(t *testing.T) {
	data := map[string]map[string]map[string]string{}
	result, diags := BuildEnrichedDynamic(data)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	if result.IsNull() {
		t.Fatal("expected non-null (empty object)")
	}
}

// ── ValidateAndEnrich integration ───────────────────────────────────────────

func TestValidateAndEnrich_NoSchema(t *testing.T) {
	// When no schema, entries pass through as-is
	entryObj, diags := types.ObjectValue(
		map[string]attr.Type{"name": types.StringType, "location": types.StringType},
		map[string]attr.Value{
			"name":     types.StringValue("rg-test"),
			"location": types.StringValue("westeurope"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	catObj, diags := types.ObjectValue(
		map[string]attr.Type{"rg1": entryObj.Type(nil)},
		map[string]attr.Value{"rg1": entryObj},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	externals, diags := types.ObjectValue(
		map[string]attr.Type{"resource_groups": catObj.Type(nil)},
		map[string]attr.Value{"resource_groups": catObj},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	data, structErrors, fatalErr := ValidateAndEnrich(externals, nil, &ProviderTokens{})
	if fatalErr != "" {
		t.Fatalf("fatal: %s", fatalErr)
	}
	if len(structErrors) > 0 {
		t.Fatalf("struct errors: %v", structErrors)
	}
	if data["resource_groups"]["rg1"]["name"] != "rg-test" {
		t.Errorf("name = %q", data["resource_groups"]["rg1"]["name"])
	}
}

func TestValidateAndEnrich_WithHTTPEnrichment(t *testing.T) {
	// Set up mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"rg-test","location":"westeurope","properties":{"provisioningState":"Succeeded"}}`)
	}))
	defer srv.Close()

	// Build externals with a schema pointing to our test server
	schemaObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"api":         types.StringType,
			"path":        types.StringType,
			"api_version": types.StringType,
		},
		map[string]attr.Value{
			"api":         types.StringValue("arm"),
			"path":        types.StringValue("/test/{name}"),
			"api_version": types.StringValue("2022-09-01"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	entryObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"name":                 types.StringType,
			"_exported_attributes": types.StringType,
		},
		map[string]attr.Value{
			"name":                 types.StringValue("rg-test"),
			"_exported_attributes": types.StringValue("*"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	catObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"_schema": schemaObj.Type(nil),
			"rg1":     entryObj.Type(nil),
		},
		map[string]attr.Value{
			"_schema": schemaObj,
			"rg1":     entryObj,
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	externals, diags := types.ObjectValue(
		map[string]attr.Type{"resource_groups": catObj.Type(nil)},
		map[string]attr.Value{"resource_groups": catObj},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	// Override API endpoint to point to test server
	origEndpoint := apiEndpoints["arm"]
	apiEndpoints["arm"] = apiEndpoint{baseURL: srv.URL, authScheme: "Bearer"}
	defer func() { apiEndpoints["arm"] = origEndpoint }()

	tokens := &ProviderTokens{ARMToken: "test-token"}
	data, structErrors, fatalErr := ValidateAndEnrich(externals, nil, tokens)
	if fatalErr != "" {
		t.Fatalf("fatal: %s", fatalErr)
	}
	if len(structErrors) > 0 {
		t.Fatalf("struct errors: %v", structErrors)
	}

	entry := data["resource_groups"]["rg1"]
	if entry["location"] != "westeurope" {
		t.Errorf("location = %q, want westeurope", entry["location"])
	}
	if entry["provisioning_state"] != "Succeeded" {
		t.Errorf("provisioning_state = %q, want Succeeded", entry["provisioning_state"])
	}
}

func TestValidateAndEnrich_StructuralError(t *testing.T) {
	// Schema that only allows specific attributes
	schemaObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"api":  types.StringType,
			"path": types.StringType,
		},
		map[string]attr.Value{
			"api":  types.StringValue("arm"),
			"path": types.StringValue("/subscriptions/{subscription_id}"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	entryObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"subscription_id": types.StringType,
			"bad_attribute":   types.StringType,
		},
		map[string]attr.Value{
			"subscription_id": types.StringValue("sub-123"),
			"bad_attribute":   types.StringValue("oops"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	catObj, diags := types.ObjectValue(
		map[string]attr.Type{
			"_schema": schemaObj.Type(nil),
			"entry1":  entryObj.Type(nil),
		},
		map[string]attr.Value{
			"_schema": schemaObj,
			"entry1":  entryObj,
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	externals, diags := types.ObjectValue(
		map[string]attr.Type{"subs": catObj.Type(nil)},
		map[string]attr.Value{"subs": catObj},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	data, structErrors, fatalErr := ValidateAndEnrich(externals, nil, &ProviderTokens{})
	if fatalErr != "" {
		t.Fatalf("fatal: %s", fatalErr)
	}
	if len(structErrors) == 0 {
		t.Fatal("expected structural errors for unknown attribute")
	}
	// The entry should still be in the result
	if data["subs"]["entry1"]["subscription_id"] != "sub-123" {
		t.Errorf("subscription_id missing from result")
	}
}

func TestValidateAndEnrich_NullExternals(t *testing.T) {
	data, structErrors, fatalErr := ValidateAndEnrich(types.ObjectNull(map[string]attr.Type{}), nil, &ProviderTokens{})
	if fatalErr != "" {
		t.Fatalf("fatal: %s", fatalErr)
	}
	if len(structErrors) > 0 {
		t.Fatalf("struct errors: %v", structErrors)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data for null externals, got %v", data)
	}
}
