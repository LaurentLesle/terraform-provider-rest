package functions

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ── walkDynamic ─────────────────────────────────────────────────────────────

func TestWalkDynamic_SimpleObject(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"name": types.StringType},
		map[string]attr.Value{"name": types.StringValue("test")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	got, err := walkDynamic(obj, []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(types.String)
	if !ok || s.ValueString() != "test" {
		t.Errorf("got %v, want 'test'", got)
	}
}

func TestWalkDynamic_NestedObject(t *testing.T) {
	inner, diags := types.ObjectValue(
		map[string]attr.Type{"location": types.StringType},
		map[string]attr.Value{"location": types.StringValue("westeurope")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	outer, diags := types.ObjectValue(
		map[string]attr.Type{"rg1": inner.Type(nil)},
		map[string]attr.Value{"rg1": inner},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	got, err := walkDynamic(outer, []string{"rg1", "location"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(types.String)
	if !ok || s.ValueString() != "westeurope" {
		t.Errorf("got %v", got)
	}
}

func TestWalkDynamic_Map(t *testing.T) {
	m, diags := types.MapValue(types.StringType, map[string]attr.Value{
		"key1": types.StringValue("value1"),
	})
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	got, err := walkDynamic(m, []string{"key1"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(types.String)
	if !ok || s.ValueString() != "value1" {
		t.Errorf("got %v", got)
	}
}

func TestWalkDynamic_KeyNotFound(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"a": types.StringType},
		map[string]attr.Value{"a": types.StringValue("1")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	_, err := walkDynamic(obj, []string{"missing"})
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestWalkDynamic_NullValue(t *testing.T) {
	_, err := walkDynamic(types.ObjectNull(map[string]attr.Type{}), []string{"key"})
	if err == nil {
		t.Error("expected error for null value")
	}
}

func TestWalkDynamic_IndexIntoScalar(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"name": types.StringType},
		map[string]attr.Value{"name": types.StringValue("test")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	_, err := walkDynamic(obj, []string{"name", "sub"})
	if err == nil {
		t.Error("expected error when indexing into a string")
	}
}

func TestWalkDynamic_UnknownPropagates(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"val": types.StringType},
		map[string]attr.Value{"val": types.StringUnknown()},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	got, err := walkDynamic(obj, []string{"val", "deeper"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsUnknown() {
		t.Error("expected unknown to propagate")
	}
}

func TestWalkDynamic_Dynamic(t *testing.T) {
	inner, diags := types.ObjectValue(
		map[string]attr.Type{"x": types.StringType},
		map[string]attr.Value{"x": types.StringValue("found")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	dyn := types.DynamicValue(inner)
	got, err := walkDynamic(dyn, []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(types.String)
	if !ok || s.ValueString() != "found" {
		t.Errorf("got %v", got)
	}
}

// ── joinKeys ────────────────────────────────────────────────────────────────

func TestJoinKeys(t *testing.T) {
	m := map[string]int{"b": 2, "a": 1}
	got := joinKeys(m)
	// Order doesn't matter, just check both keys are present
	if got != "a, b" && got != "b, a" {
		t.Errorf("got %q", got)
	}
}

func TestJoinKeys_Empty(t *testing.T) {
	got := joinKeys(map[string]string{})
	if got != "<empty>" {
		t.Errorf("got %q, want '<empty>'", got)
	}
}

// ── resolveValue coverage (extend existing tests) ───────────────────────────

func TestResolveValue_MapWithRefs(t *testing.T) {
	ctx := buildContext(t)

	m, diags := types.MapValue(types.StringType, map[string]attr.Value{
		"loc":    types.StringValue("ref:resource_groups.group1.location"),
		"static": types.StringValue("unchanged"),
	})
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	got, err := resolveValue(ctx, m)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the map was resolved — it should not error
	if got == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestResolveValue_TupleWithRefs(t *testing.T) {
	ctx := buildContext(t)

	tuple, diags := types.TupleValue(
		[]attr.Type{types.StringType, types.StringType},
		[]attr.Value{
			types.StringValue("ref:resource_groups.group1.name"),
			types.StringValue("literal"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	got, err := resolveValue(ctx, tuple)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestResolveValue_DynamicWrapped(t *testing.T) {
	ctx := buildContext(t)

	inner := types.StringValue("ref:resource_groups.group1.location")
	dyn := types.DynamicValue(inner)

	got, err := resolveValue(ctx, dyn)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(types.String)
	if !ok || s.ValueString() != "westeurope" {
		t.Errorf("got %v", got)
	}
}

func TestResolveValue_BoolPassthrough(t *testing.T) {
	got, err := resolveValue(types.StringValue("unused"), types.BoolValue(true))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := got.(types.Bool); !ok || !b.ValueBool() {
		t.Errorf("expected true, got %v", got)
	}
}

// ── wrapDynamic / wrapDynamicList ───────────────────────────────────────────

func TestWrapDynamic(t *testing.T) {
	input := map[string]attr.Value{
		"a": types.StringValue("hello"),
		"b": types.StringValue("world"),
	}
	got := wrapDynamic(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	for _, v := range got {
		if _, ok := v.(types.Dynamic); !ok {
			t.Errorf("expected Dynamic wrapper, got %T", v)
		}
	}
}

func TestWrapDynamicList(t *testing.T) {
	input := []attr.Value{types.StringValue("a"), types.StringValue("b")}
	got := wrapDynamicList(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(got))
	}
	for _, v := range got {
		if _, ok := v.(types.Dynamic); !ok {
			t.Errorf("expected Dynamic wrapper, got %T", v)
		}
	}
}
