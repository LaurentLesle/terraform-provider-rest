package functions

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// helper: build a simple context object like { group1: { location: "westeurope", tags: { env: "dev" } } }
func buildContext(t *testing.T) attr.Value {
	t.Helper()

	tags, diags := types.ObjectValue(
		map[string]attr.Type{"env": types.StringType, "team": types.StringType},
		map[string]attr.Value{
			"env":  types.StringValue("dev"),
			"team": types.StringValue("infra"),
		},
	)
	if diags.HasError() {
		t.Fatalf("building tags: %s", diags.Errors())
	}

	group1, diags := types.ObjectValue(
		map[string]attr.Type{
			"location": types.StringType,
			"name":     types.StringType,
			"tags":     tags.Type(nil),
		},
		map[string]attr.Value{
			"location": types.StringValue("westeurope"),
			"name":     types.StringValue("rg-test"),
			"tags":     tags,
		},
	)
	if diags.HasError() {
		t.Fatalf("building group1: %s", diags.Errors())
	}

	tenants, diags := types.ObjectValue(
		map[string]attr.Type{"tenant_id": types.StringType},
		map[string]attr.Value{"tenant_id": types.StringValue("aaaa-bbbb-cccc")},
	)
	if diags.HasError() {
		t.Fatalf("building tenants: %s", diags.Errors())
	}

	externals, diags := types.ObjectValue(
		map[string]attr.Type{"primary": tenants.Type(nil)},
		map[string]attr.Value{"primary": tenants},
	)
	if diags.HasError() {
		t.Fatalf("building externals: %s", diags.Errors())
	}

	groups, diags := types.ObjectValue(
		map[string]attr.Type{"group1": group1.Type(nil)},
		map[string]attr.Value{"group1": group1},
	)
	if diags.HasError() {
		t.Fatalf("building groups: %s", diags.Errors())
	}

	ctx, diags := types.ObjectValue(
		map[string]attr.Type{
			"resource_groups": groups.Type(nil),
			"azure_tenants":   externals.Type(nil),
		},
		map[string]attr.Value{
			"resource_groups": groups,
			"azure_tenants":   externals,
		},
	)
	if diags.HasError() {
		t.Fatalf("building context: %s", diags.Errors())
	}
	return ctx
}

// ── resolveSingleRef tests ──────────────────────────────────────────────────

func TestResolveSingleRef_Simple(t *testing.T) {
	ctx := buildContext(t)
	got, err := resolveSingleRef(ctx, "ref:resource_groups.group1.location")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	if s.ValueString() != "westeurope" {
		t.Errorf("got %q, want %q", s.ValueString(), "westeurope")
	}
}

func TestResolveSingleRef_NestedPath(t *testing.T) {
	ctx := buildContext(t)
	got, err := resolveSingleRef(ctx, "ref:resource_groups.group1.tags.env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	if s.ValueString() != "dev" {
		t.Errorf("got %q, want %q", s.ValueString(), "dev")
	}
}

func TestResolveSingleRef_NotFound(t *testing.T) {
	ctx := buildContext(t)
	_, err := resolveSingleRef(ctx, "ref:resource_groups.group1.nonexistent")
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

func TestResolveSingleRef_DefaultValue(t *testing.T) {
	ctx := buildContext(t)
	got, err := resolveSingleRef(ctx, "ref:resource_groups.group1.nonexistent|fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	if s.ValueString() != "fallback" {
		t.Errorf("got %q, want %q", s.ValueString(), "fallback")
	}
}

// ── resolveInterpolatedRef tests ────────────────────────────────────────────

func TestResolveInterpolatedRef_Single(t *testing.T) {
	ctx := buildContext(t)
	got, err := resolveInterpolatedRef(ctx, "https://login.microsoftonline.com/${ref:azure_tenants.primary.tenant_id}/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	want := "https://login.microsoftonline.com/aaaa-bbbb-cccc/"
	if s.ValueString() != want {
		t.Errorf("got %q, want %q", s.ValueString(), want)
	}
}

func TestResolveInterpolatedRef_Multiple(t *testing.T) {
	ctx := buildContext(t)
	got, err := resolveInterpolatedRef(ctx, "${ref:resource_groups.group1.name} in ${ref:resource_groups.group1.location}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	want := "rg-test in westeurope"
	if s.ValueString() != want {
		t.Errorf("got %q, want %q", s.ValueString(), want)
	}
}

func TestResolveInterpolatedRef_WithDefault(t *testing.T) {
	ctx := buildContext(t)
	got, err := resolveInterpolatedRef(ctx, "prefix-${ref:resource_groups.group1.missing|unknown}-suffix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	want := "prefix-unknown-suffix"
	if s.ValueString() != want {
		t.Errorf("got %q, want %q", s.ValueString(), want)
	}
}

func TestResolveInterpolatedRef_Unterminated(t *testing.T) {
	ctx := buildContext(t)
	_, err := resolveInterpolatedRef(ctx, "https://example.com/${ref:azure_tenants.primary.tenant_id")
	if err == nil {
		t.Fatal("expected error for unterminated interpolation, got nil")
	}
}

func TestResolveInterpolatedRef_MissingKeyNoDefault(t *testing.T) {
	ctx := buildContext(t)
	_, err := resolveInterpolatedRef(ctx, "https://example.com/${ref:azure_tenants.primary.nonexistent}/")
	if err == nil {
		t.Fatal("expected error for missing key without default, got nil")
	}
}

// ── resolveValue integration tests ──────────────────────────────────────────

func TestResolveValue_PlainString(t *testing.T) {
	ctx := buildContext(t)
	input := types.StringValue("no refs here")
	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	if s.ValueString() != "no refs here" {
		t.Errorf("got %q, want %q", s.ValueString(), "no refs here")
	}
}

func TestResolveValue_FullRef(t *testing.T) {
	ctx := buildContext(t)
	input := types.StringValue("ref:resource_groups.group1.location")
	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	if s.ValueString() != "westeurope" {
		t.Errorf("got %q, want %q", s.ValueString(), "westeurope")
	}
}

func TestResolveValue_InterpolatedRef(t *testing.T) {
	ctx := buildContext(t)
	input := types.StringValue("https://sts.windows.net/${ref:azure_tenants.primary.tenant_id}/")
	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(types.String)
	if !ok {
		t.Fatalf("expected String, got %T", got)
	}
	want := "https://sts.windows.net/aaaa-bbbb-cccc/"
	if s.ValueString() != want {
		t.Errorf("got %q, want %q", s.ValueString(), want)
	}
}

func TestResolveValue_ObjectWithRefs(t *testing.T) {
	ctx := buildContext(t)

	input, diags := types.ObjectValue(
		map[string]attr.Type{
			"location": types.StringType,
			"endpoint": types.StringType,
			"plain":    types.StringType,
		},
		map[string]attr.Value{
			"location": types.StringValue("ref:resource_groups.group1.location"),
			"endpoint": types.StringValue("https://login.microsoftonline.com/${ref:azure_tenants.primary.tenant_id}/"),
			"plain":    types.StringValue("static-value"),
		},
	)
	if diags.HasError() {
		t.Fatalf("building input: %s", diags.Errors())
	}

	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj, ok := got.(types.Object)
	if !ok {
		t.Fatalf("expected Object, got %T", got)
	}

	attrs := obj.Attributes()

	// Check location (full ref)
	locDyn, ok := attrs["location"].(types.Dynamic)
	if !ok {
		t.Fatalf("location: expected Dynamic, got %T", attrs["location"])
	}
	locStr, ok := locDyn.UnderlyingValue().(types.String)
	if !ok {
		t.Fatalf("location: expected String inside Dynamic, got %T", locDyn.UnderlyingValue())
	}
	if locStr.ValueString() != "westeurope" {
		t.Errorf("location: got %q, want %q", locStr.ValueString(), "westeurope")
	}

	// Check endpoint (interpolated ref)
	epDyn, ok := attrs["endpoint"].(types.Dynamic)
	if !ok {
		t.Fatalf("endpoint: expected Dynamic, got %T", attrs["endpoint"])
	}
	epStr, ok := epDyn.UnderlyingValue().(types.String)
	if !ok {
		t.Fatalf("endpoint: expected String inside Dynamic, got %T", epDyn.UnderlyingValue())
	}
	wantEp := "https://login.microsoftonline.com/aaaa-bbbb-cccc/"
	if epStr.ValueString() != wantEp {
		t.Errorf("endpoint: got %q, want %q", epStr.ValueString(), wantEp)
	}

	// Check plain (passthrough)
	plainDyn, ok := attrs["plain"].(types.Dynamic)
	if !ok {
		t.Fatalf("plain: expected Dynamic, got %T", attrs["plain"])
	}
	plainStr, ok := plainDyn.UnderlyingValue().(types.String)
	if !ok {
		t.Fatalf("plain: expected String inside Dynamic, got %T", plainDyn.UnderlyingValue())
	}
	if plainStr.ValueString() != "static-value" {
		t.Errorf("plain: got %q, want %q", plainStr.ValueString(), "static-value")
	}
}

func TestResolveValue_NullAndNil(t *testing.T) {
	ctx := buildContext(t)

	got, err := resolveValue(ctx, nil)
	if err != nil {
		t.Fatalf("nil: unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("nil: expected nil, got %v", got)
	}

	null := types.StringNull()
	got, err = resolveValue(ctx, null)
	if err != nil {
		t.Fatalf("null: unexpected error: %v", err)
	}
	if !got.IsNull() {
		t.Errorf("null: expected null, got %v", got)
	}
}

// ── Unknown value propagation tests (body = known after apply regression) ───
//
// When a module output (e.g. tags) is unknown at plan time, any ref: pointing
// to it should resolve to unknown. This causes the entire body to become
// (known after apply). The fix is to echo known input values instead of
// unknown API-response values in module outputs.

// buildContextWithUnknownTags simulates the regression: tags come from
// rest_resource.output.tags which is unknown at plan time.
func buildContextWithUnknownTags(t *testing.T) attr.Value {
	t.Helper()

	// tags is UNKNOWN — simulates rest_resource.resource_group.output.tags
	unknownTags := types.ObjectUnknown(
		map[string]attr.Type{"env": types.StringType, "team": types.StringType},
	)

	group1, diags := types.ObjectValue(
		map[string]attr.Type{
			"location": types.StringType,
			"name":     types.StringType,
			"tags":     unknownTags.Type(nil),
		},
		map[string]attr.Value{
			"location": types.StringValue("westeurope"),
			"name":     types.StringValue("rg-test"),
			"tags":     unknownTags,
		},
	)
	if diags.HasError() {
		t.Fatalf("building group1: %s", diags.Errors())
	}

	groups, diags := types.ObjectValue(
		map[string]attr.Type{"group1": group1.Type(nil)},
		map[string]attr.Value{"group1": group1},
	)
	if diags.HasError() {
		t.Fatalf("building groups: %s", diags.Errors())
	}

	ctx, diags := types.ObjectValue(
		map[string]attr.Type{"resource_groups": groups.Type(nil)},
		map[string]attr.Value{"resource_groups": groups},
	)
	if diags.HasError() {
		t.Fatalf("building context: %s", diags.Errors())
	}
	return ctx
}

// buildContextWithKnownTags simulates the fix: tags echoed from var.tags (known).
func buildContextWithKnownTags(t *testing.T) attr.Value {
	t.Helper()
	return buildContext(t) // already has known tags
}

func TestResolveValue_UnknownTagsPropagate(t *testing.T) {
	// REGRESSION: when tags are unknown in context, ref:...tags resolves to unknown
	ctx := buildContextWithUnknownTags(t)
	input := types.StringValue("ref:resource_groups.group1.tags")
	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsUnknown() {
		t.Errorf("expected unknown value (regression: tags from output are unknown at plan time), got known: %v", got)
	}
}

func TestResolveValue_KnownTagsResolve(t *testing.T) {
	// FIX: when tags are known in context (echoed from input), ref:...tags resolves to known
	ctx := buildContextWithKnownTags(t)
	input := types.StringValue("ref:resource_groups.group1.tags")
	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IsUnknown() {
		t.Errorf("expected known value (fix: tags from input are known at plan time), got unknown")
	}
}

func TestResolveValue_UnknownTagsMakeBodyUnknown(t *testing.T) {
	// Full body simulation: an object where tags ref points to unknown context value.
	// This is the exact scenario that causes body = (known after apply).
	ctx := buildContextWithUnknownTags(t)

	input, diags := types.ObjectValue(
		map[string]attr.Type{
			"location": types.StringType,
			"tags":     types.StringType,
		},
		map[string]attr.Value{
			"location": types.StringValue("ref:resource_groups.group1.location"),
			"tags":     types.StringValue("ref:resource_groups.group1.tags"),
		},
	)
	if diags.HasError() {
		t.Fatalf("building input: %s", diags.Errors())
	}

	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj, ok := got.(types.Object)
	if !ok {
		t.Fatalf("expected Object, got %T", got)
	}

	attrs := obj.Attributes()

	// location should still be known
	locDyn := attrs["location"].(types.Dynamic)
	if locDyn.UnderlyingValue().IsUnknown() {
		t.Error("location should be known even when tags are unknown")
	}

	// tags should be unknown — this is what causes body = (known after apply)
	tagsDyn := attrs["tags"].(types.Dynamic)
	if !tagsDyn.UnderlyingValue().IsUnknown() {
		t.Error("tags should be unknown when context tags come from API output")
	}
}

func TestResolveValue_KnownTagsKeepBodyKnown(t *testing.T) {
	// With the fix: tags from input are known, so body stays fully known at plan time.
	ctx := buildContextWithKnownTags(t)

	input, diags := types.ObjectValue(
		map[string]attr.Type{
			"location": types.StringType,
			"tags":     types.StringType,
		},
		map[string]attr.Value{
			"location": types.StringValue("ref:resource_groups.group1.location"),
			"tags":     types.StringValue("ref:resource_groups.group1.tags"),
		},
	)
	if diags.HasError() {
		t.Fatalf("building input: %s", diags.Errors())
	}

	got, err := resolveValue(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj, ok := got.(types.Object)
	if !ok {
		t.Fatalf("expected Object, got %T", got)
	}

	attrs := obj.Attributes()

	// Both location and tags should be known
	locDyn := attrs["location"].(types.Dynamic)
	if locDyn.UnderlyingValue().IsUnknown() {
		t.Error("location should be known")
	}

	tagsDyn := attrs["tags"].(types.Dynamic)
	if tagsDyn.UnderlyingValue().IsUnknown() {
		t.Error("tags should be known when echoed from input (fix for body = known after apply)")
	}
}

// ── Error message quality tests ─────────────────────────────────────────────

func TestResolveSingleRef_EmptyObjectShowsEmptyHint(t *testing.T) {
	// Empty remote_states → error should say <empty> not just blank
	emptyRS, diags := types.ObjectValue(
		map[string]attr.Type{},
		map[string]attr.Value{},
	)
	if diags.HasError() {
		t.Fatalf("building empty rs: %s", diags.Errors())
	}

	ctx, diags := types.ObjectValue(
		map[string]attr.Type{"remote_states": emptyRS.Type(nil)},
		map[string]attr.Value{"remote_states": emptyRS},
	)
	if diags.HasError() {
		t.Fatalf("building ctx: %s", diags.Errors())
	}

	_, err := resolveSingleRef(ctx, "ref:remote_states.bootstrap.azure_values")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()

	if !strings.Contains(errMsg, "<empty>") {
		t.Errorf("error should mention <empty> for empty object, got: %s", errMsg)
	}
}

func TestResolveSingleRef_RemoteStatesHint(t *testing.T) {
	// When ref starts with remote_states and fails, error should hint about the vars
	emptyRS, diags := types.ObjectValue(
		map[string]attr.Type{},
		map[string]attr.Value{},
	)
	if diags.HasError() {
		t.Fatalf("building empty rs: %s", diags.Errors())
	}

	ctx, diags := types.ObjectValue(
		map[string]attr.Type{"remote_states": emptyRS.Type(nil)},
		map[string]attr.Value{"remote_states": emptyRS},
	)
	if diags.HasError() {
		t.Fatalf("building ctx: %s", diags.Errors())
	}

	_, err := resolveSingleRef(ctx, "ref:remote_states.bootstrap.azure_values.azure_storage_accounts.tfstate.id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()

	if !strings.Contains(errMsg, "remote_state_backend") {
		t.Errorf("error should hint about remote_state_backend var, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "remote_state_keys") {
		t.Errorf("error should hint about remote_state_keys var, got: %s", errMsg)
	}
}

func TestResolveSingleRef_NonRemoteStatesNoHint(t *testing.T) {
	// For non-remote_states refs, no remote_state hint should appear
	ctx := buildContext(t)
	_, err := resolveSingleRef(ctx, "ref:resource_groups.nonexistent.location")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()

	if strings.Contains(errMsg, "remote_state_backend") {
		t.Errorf("non-remote_states error should NOT hint about remote_state_backend, got: %s", errMsg)
	}
}
