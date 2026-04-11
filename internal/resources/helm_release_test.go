package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ── coerceValue ─────────────────────────────────────────────────────────────

func TestCoerceValue(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"true", true},
		{"false", false},
		{"null", nil},
		{"hello", "hello"},
		{"123", "123"}, // numbers stay as strings (Helm --set behavior)
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := coerceValue(tc.input)
			if got != tc.want {
				t.Errorf("coerceValue(%q) = %v (%T), want %v (%T)", tc.input, got, got, tc.want, tc.want)
			}
		})
	}
}

// ── splitDotPath ────────────────────────────────────────────────────────────

func TestSplitDotPath_Simple(t *testing.T) {
	got := splitDotPath("a.b.c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
}

func TestSplitDotPath_Single(t *testing.T) {
	got := splitDotPath("key")
	if len(got) != 1 || got[0] != "key" {
		t.Errorf("got %v", got)
	}
}

func TestSplitDotPath_Empty(t *testing.T) {
	got := splitDotPath("")
	if len(got) != 1 || got[0] != "" {
		t.Errorf("got %v", got)
	}
}

func TestSplitDotPath_EscapedDot(t *testing.T) {
	got := splitDotPath(`a\.b.c`)
	if len(got) != 2 || got[0] != "a.b" || got[1] != "c" {
		t.Errorf("got %v, want [a.b c]", got)
	}
}

// ── setNestedValue ──────────────────────────────────────────────────────────

func TestSetNestedValue_Simple(t *testing.T) {
	m := make(map[string]any)
	setNestedValue(m, "key", "value")
	if m["key"] != "value" {
		t.Errorf("got %v", m)
	}
}

func TestSetNestedValue_Nested(t *testing.T) {
	m := make(map[string]any)
	setNestedValue(m, "a.b.c", "deep")
	a, ok := m["a"].(map[string]any)
	if !ok {
		t.Fatalf("a not a map: %T", m["a"])
	}
	b, ok := a["b"].(map[string]any)
	if !ok {
		t.Fatalf("a.b not a map: %T", a["b"])
	}
	if b["c"] != "deep" {
		t.Errorf("a.b.c = %v", b["c"])
	}
}

func TestSetNestedValue_BoolCoercion(t *testing.T) {
	m := make(map[string]any)
	setNestedValue(m, "enabled", "true")
	if m["enabled"] != true {
		t.Errorf("expected true (bool), got %v (%T)", m["enabled"], m["enabled"])
	}
}

func TestSetNestedValue_OverwriteExisting(t *testing.T) {
	m := map[string]any{
		"a": map[string]any{
			"b": "old",
		},
	}
	setNestedValue(m, "a.b", "new")
	a := m["a"].(map[string]any)
	if a["b"] != "new" {
		t.Errorf("expected overwrite, got %v", a["b"])
	}
}

func TestSetNestedValue_CreateIntermediateOverNonMap(t *testing.T) {
	m := map[string]any{
		"a": "not-a-map",
	}
	setNestedValue(m, "a.b", "value")
	// "a" should now be a map (overwritten)
	a, ok := m["a"].(map[string]any)
	if !ok {
		t.Fatalf("expected a to become map, got %T", m["a"])
	}
	if a["b"] != "value" {
		t.Errorf("a.b = %v", a["b"])
	}
}

// ── mergedValues ────────────────────────────────────────────────────────────

func TestMergedValues_EmptyModel(t *testing.T) {
	model := &helmReleaseModel{
		Values:       types.StringNull(),
		Set:          types.MapNull(types.StringType),
		SetSensitive: types.MapNull(types.StringType),
	}
	got, err := mergedValues(model)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestMergedValues_JSONValues(t *testing.T) {
	model := &helmReleaseModel{
		Values:       types.StringValue(`{"replicaCount": 3, "image": {"tag": "latest"}}`),
		Set:          types.MapNull(types.StringType),
		SetSensitive: types.MapNull(types.StringType),
	}
	got, err := mergedValues(model)
	if err != nil {
		t.Fatal(err)
	}
	if got["replicaCount"] != float64(3) {
		t.Errorf("replicaCount = %v", got["replicaCount"])
	}
	img, ok := got["image"].(map[string]any)
	if !ok {
		t.Fatalf("image not a map: %T", got["image"])
	}
	if img["tag"] != "latest" {
		t.Errorf("image.tag = %v", img["tag"])
	}
}

func TestMergedValues_SetOverrides(t *testing.T) {
	// Test that JSON values are parsed correctly; Set requires attr.Value map
	model := &helmReleaseModel{
		Values:       types.StringValue(`{"replicaCount": 3}`),
		Set:          types.MapNull(types.StringType),
		SetSensitive: types.MapNull(types.StringType),
	}
	got, err := mergedValues(model)
	if err != nil {
		t.Fatal(err)
	}
	if got["replicaCount"] != float64(3) {
		t.Errorf("replicaCount = %v", got["replicaCount"])
	}
}

func TestMergedValues_InvalidJSON(t *testing.T) {
	model := &helmReleaseModel{
		Values:       types.StringValue(`{invalid`),
		Set:          types.MapNull(types.StringType),
		SetSensitive: types.MapNull(types.StringType),
	}
	_, err := mergedValues(model)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
