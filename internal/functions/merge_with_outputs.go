package functions

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ function.Function = &MergeWithOutputsFunction{}

type MergeWithOutputsFunction struct{}

func NewMergeWithOutputsFunction() function.Function {
	return &MergeWithOutputsFunction{}
}

func (f *MergeWithOutputsFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "merge_with_outputs"
}

func (f *MergeWithOutputsFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Merge resource config entries with their module outputs per key",
		Description: "Takes a map of resource config entries and a map of module outputs " +
			"(keyed by the same for_each keys). For each key, merges the config attributes " +
			"with the output attributes (outputs take precedence on collision). Returns " +
			"the merged map, suitable for use as a ref: resolution context layer.",
		Parameters: []function.Parameter{
			function.DynamicParameter{
				Name:               "config",
				AllowUnknownValues: true,
				Description:        "Map of resolved resource config entries (e.g. local.resource_groups).",
			},
			function.DynamicParameter{
				Name:               "outputs",
				AllowUnknownValues: true,
				Description:        "Map of module outputs (e.g. module.resource_groups). Must have the same keys as config.",
			},
		},
		Return: function.DynamicReturn{},
	}
}

func (f *MergeWithOutputsFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var configVal types.Dynamic
	var outputsVal types.Dynamic

	resp.Error = function.ConcatFuncErrors(
		req.Arguments.Get(ctx, &configVal, &outputsVal),
	)
	if resp.Error != nil {
		return
	}

	configUnderlying := configVal.UnderlyingValue()
	outputsUnderlying := outputsVal.UnderlyingValue()

	if configUnderlying == nil || configUnderlying.IsNull() {
		empty, diags := types.ObjectValue(map[string]attr.Type{}, map[string]attr.Value{})
		if diags.HasError() {
			resp.Error = function.NewFuncError(fmt.Sprintf("creating empty object: %s", diags.Errors()))
			return
		}
		resp.Error = function.ConcatFuncErrors(resp.Result.Set(ctx, types.DynamicValue(empty)))
		return
	}

	configEntries := extractEntries(configUnderlying)
	outputEntries := extractEntries(outputsUnderlying)

	resultAttrs := make(map[string]attr.Value, len(configEntries))
	resultTypes := make(map[string]attr.Type, len(configEntries))

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

		obj, diags := types.ObjectValue(mergedTypes, mergedValues)
		if diags.HasError() {
			resp.Error = function.NewFuncError(fmt.Sprintf("key %q: %s", k, diags.Errors()))
			return
		}
		resultAttrs[k] = types.DynamicValue(obj)
		resultTypes[k] = types.DynamicType
	}

	result, diags := types.ObjectValue(resultTypes, resultAttrs)
	if diags.HasError() {
		resp.Error = function.NewFuncError(fmt.Sprintf("building result: %s", diags.Errors()))
		return
	}

	resp.Error = function.ConcatFuncErrors(resp.Result.Set(ctx, types.DynamicValue(result)))
}

func extractEntries(val attr.Value) map[string]attr.Value {
	if val == nil || val.IsNull() || val.IsUnknown() {
		return map[string]attr.Value{}
	}
	switch v := val.(type) {
	case types.Object:
		return v.Attributes()
	case types.Map:
		return v.Elements()
	case types.Dynamic:
		return extractEntries(v.UnderlyingValue())
	default:
		return map[string]attr.Value{}
	}
}

func extractAttrs(val attr.Value) map[string]attr.Value {
	if val == nil || val.IsNull() || val.IsUnknown() {
		return map[string]attr.Value{}
	}
	switch v := val.(type) {
	case types.Object:
		src := v.Attributes()
		out := make(map[string]attr.Value, len(src))
		for k, a := range src {
			out[k] = a
		}
		return out
	case types.Map:
		src := v.Elements()
		out := make(map[string]attr.Value, len(src))
		for k, e := range src {
			out[k] = e
		}
		return out
	case types.Dynamic:
		return extractAttrs(v.UnderlyingValue())
	default:
		return map[string]attr.Value{}
	}
}
