package functions

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ── extractEntries ──────────────────────────────────────────────────────────

func TestExtractEntries_Object(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"a": types.StringType, "b": types.StringType},
		map[string]attr.Value{
			"a": types.StringValue("1"),
			"b": types.StringValue("2"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractEntries(obj)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
}

func TestExtractEntries_Map(t *testing.T) {
	m, diags := types.MapValue(types.StringType, map[string]attr.Value{
		"x": types.StringValue("hello"),
	})
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractEntries(m)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestExtractEntries_Dynamic(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"k": types.StringType},
		map[string]attr.Value{"k": types.StringValue("v")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	dyn := types.DynamicValue(obj)
	got := extractEntries(dyn)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry via Dynamic, got %d", len(got))
	}
}

func TestExtractEntries_Null(t *testing.T) {
	got := extractEntries(nil)
	if len(got) != 0 {
		t.Errorf("expected 0 entries for nil, got %d", len(got))
	}
	got = extractEntries(types.ObjectNull(map[string]attr.Type{}))
	if len(got) != 0 {
		t.Errorf("expected 0 entries for null, got %d", len(got))
	}
}

func TestExtractEntries_UnsupportedType(t *testing.T) {
	got := extractEntries(types.StringValue("not an object"))
	if len(got) != 0 {
		t.Errorf("expected 0 entries for String, got %d", len(got))
	}
}

// ── extractAttrs ────────────────────────────────────────────────────────────

func TestExtractAttrs_Object(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"name": types.StringType},
		map[string]attr.Value{"name": types.StringValue("test")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractAttrs(obj)
	if len(got) != 1 {
		t.Fatalf("expected 1 attr, got %d", len(got))
	}
	if s, ok := got["name"].(types.String); !ok || s.ValueString() != "test" {
		t.Errorf("name = %v", got["name"])
	}
}

func TestExtractAttrs_Map(t *testing.T) {
	m, diags := types.MapValue(types.StringType, map[string]attr.Value{
		"k1": types.StringValue("v1"),
		"k2": types.StringValue("v2"),
	})
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractAttrs(m)
	if len(got) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(got))
	}
}

func TestExtractAttrs_Null(t *testing.T) {
	got := extractAttrs(nil)
	if len(got) != 0 {
		t.Errorf("expected 0 for nil, got %d", len(got))
	}
}

func TestExtractAttrs_Dynamic(t *testing.T) {
	obj, diags := types.ObjectValue(
		map[string]attr.Type{"a": types.StringType},
		map[string]attr.Value{"a": types.StringValue("b")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}
	got := extractAttrs(types.DynamicValue(obj))
	if len(got) != 1 {
		t.Fatalf("expected 1 via Dynamic, got %d", len(got))
	}
}

// ── MergeWithOutputsFunction helpers (integration via exported function) ────

func TestMergeWithOutputs_Basic(t *testing.T) {
	// Config: { rg1: { name: "rg-test", location: "westeurope" } }
	rg1Config, diags := types.ObjectValue(
		map[string]attr.Type{"name": types.StringType, "location": types.StringType},
		map[string]attr.Value{
			"name":     types.StringValue("rg-test"),
			"location": types.StringValue("westeurope"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	config, diags := types.ObjectValue(
		map[string]attr.Type{"rg1": rg1Config.Type(context.TODO())},
		map[string]attr.Value{"rg1": rg1Config},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	// Outputs: { rg1: { id: "/sub/rg/rg-test", location: "from-output" } }
	rg1Output, diags := types.ObjectValue(
		map[string]attr.Type{"id": types.StringType, "location": types.StringType},
		map[string]attr.Value{
			"id":       types.StringValue("/sub/rg/rg-test"),
			"location": types.StringValue("from-output"),
		},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	outputs, diags := types.ObjectValue(
		map[string]attr.Type{"rg1": rg1Output.Type(context.TODO())},
		map[string]attr.Value{"rg1": rg1Output},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	// Merge: outputs should win on collision (location)
	configEntries := extractEntries(config)
	outputEntries := extractEntries(outputs)

	resultAttrs := make(map[string]attr.Value)
	resultTypes := make(map[string]attr.Type)

	for k, cfgEntry := range configEntries {
		merged := extractAttrs(cfgEntry)
		if outEntry, ok := outputEntries[k]; ok {
			for outK, outV := range extractAttrs(outEntry) {
				merged[outK] = outV
			}
		}
		mergedTypes := make(map[string]attr.Type, len(merged))
		mergedValues := make(map[string]attr.Value, len(merged))
		for attrK, attrV := range merged {
			mergedTypes[attrK] = types.DynamicType
			mergedValues[attrK] = types.DynamicValue(attrV)
		}
		obj, d := types.ObjectValue(mergedTypes, mergedValues)
		if d.HasError() {
			t.Fatalf("key %q: %s", k, d.Errors())
		}
		resultAttrs[k] = obj
		resultTypes[k] = obj.Type(context.TODO())
	}

	// Verify merge results
	rg1Result, ok := resultAttrs["rg1"].(types.Object)
	if !ok {
		t.Fatalf("rg1 not an Object: %T", resultAttrs["rg1"])
	}
	attrs := rg1Result.Attributes()

	// name should come from config
	if name, ok := attrs["name"]; ok {
		if dv, ok := name.(types.Dynamic); ok {
			if sv, ok := dv.UnderlyingValue().(types.String); ok {
				if sv.ValueString() != "rg-test" {
					t.Errorf("name = %q, want rg-test", sv.ValueString())
				}
			}
		}
	} else {
		t.Error("name missing from merged result")
	}

	// location should come from output (override)
	if loc, ok := attrs["location"]; ok {
		if dv, ok := loc.(types.Dynamic); ok {
			if sv, ok := dv.UnderlyingValue().(types.String); ok {
				if sv.ValueString() != "from-output" {
					t.Errorf("location = %q, want from-output (output should override)", sv.ValueString())
				}
			}
		}
	} else {
		t.Error("location missing from merged result")
	}

	// id should come from output (new key)
	if id, ok := attrs["id"]; ok {
		if dv, ok := id.(types.Dynamic); ok {
			if sv, ok := dv.UnderlyingValue().(types.String); ok {
				if sv.ValueString() != "/sub/rg/rg-test" {
					t.Errorf("id = %q, want /sub/rg/rg-test", sv.ValueString())
				}
			}
		}
	} else {
		t.Error("id missing from merged result")
	}
}

func TestMergeWithOutputs_NullConfig(t *testing.T) {
	got := extractEntries(types.ObjectNull(map[string]attr.Type{}))
	if len(got) != 0 {
		t.Errorf("expected empty entries for null config, got %d", len(got))
	}
}

func TestMergeWithOutputs_NoMatchingOutput(t *testing.T) {
	configEntry, diags := types.ObjectValue(
		map[string]attr.Type{"name": types.StringType},
		map[string]attr.Value{"name": types.StringValue("test")},
	)
	if diags.HasError() {
		t.Fatal(diags.Errors())
	}

	configEntries := map[string]attr.Value{"key1": configEntry}
	outputEntries := map[string]attr.Value{} // no matching key

	merged := extractAttrs(configEntries["key1"])
	if outEntry, ok := outputEntries["key1"]; ok {
		for outK, outV := range extractAttrs(outEntry) {
			merged[outK] = outV
		}
	}

	if s, ok := merged["name"].(types.String); !ok || s.ValueString() != "test" {
		t.Errorf("config-only merge failed: %v", merged)
	}
}
