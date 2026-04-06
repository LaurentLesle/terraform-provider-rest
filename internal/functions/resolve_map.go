package functions

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ function.Function = &ResolveMapFunction{}

type ResolveMapFunction struct{}

func NewResolveMapFunction() function.Function {
	return &ResolveMapFunction{}
}

func (f *ResolveMapFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "resolve_map"
}

func (f *ResolveMapFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Resolve all ref: expressions in a map of resource instances",
		Description: `Takes a resolution context and a map of resource instances (e.g. the raw
storage_accounts map from YAML). Walks every string value in each instance;
if it starts with "ref:", resolves it against the context. List elements
with "ref:" prefixes are also resolved. Non-ref values pass through unchanged.
Returns the fully-resolved map with the same structure.`,
		Parameters: []function.Parameter{
			function.DynamicParameter{
				Name:               "context",
				AllowUnknownValues: true,
				Description:        "The resolution context map (merged module outputs + input config).",
			},
			function.DynamicParameter{
				Name:               "resources",
				AllowUnknownValues: true,
				Description:        "Map of resource instances to resolve, e.g. { mysa = { location = \"ref:resource_groups.rg1.location\", sku = \"Standard_LRS\" } }.",
			},
		},
		Return: function.DynamicReturn{},
	}
}

func (f *ResolveMapFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var contextVal types.Dynamic
	var resourcesVal types.Dynamic

	resp.Error = function.ConcatFuncErrors(
		req.Arguments.Get(ctx, &contextVal, &resourcesVal),
	)
	if resp.Error != nil {
		return
	}

	// Handle empty/null resources: return empty object
	underlying := resourcesVal.UnderlyingValue()
	if underlying == nil || underlying.IsNull() {
		empty, diags := types.ObjectValue(map[string]attr.Type{}, map[string]attr.Value{})
		if diags.HasError() {
			resp.Error = function.NewFuncError(fmt.Sprintf("creating empty object: %s", diags.Errors()))
			return
		}
		resp.Error = function.ConcatFuncErrors(
			resp.Result.Set(ctx, types.DynamicValue(empty)),
		)
		return
	}

	resolved, err := resolveValue(contextVal.UnderlyingValue(), underlying)
	if err != nil {
		resp.Error = function.NewFuncError(err.Error())
		return
	}

	if resolved == nil {
		resp.Error = function.NewFuncError(fmt.Sprintf(
			"resolve_map returned nil for type %T", underlying,
		))
		return
	}

	resp.Error = function.ConcatFuncErrors(
		resp.Result.Set(ctx, types.DynamicValue(resolved)),
	)
}

// resolveValue recursively walks an attr.Value tree. Any string starting with
// "ref:" is resolved against the context. Objects, maps, tuples, and lists
// are traversed recursively. Everything else passes through.
func resolveValue(ctxMap attr.Value, val attr.Value) (attr.Value, error) {
	if val == nil || val.IsNull() || val.IsUnknown() {
		return val, nil
	}

	switch v := val.(type) {
	case types.String:
		s := v.ValueString()
		if strings.HasPrefix(s, "ref:") {
			return resolveSingleRef(ctxMap, s)
		}
		if strings.Contains(s, "${ref:") {
			return resolveInterpolatedRef(ctxMap, s)
		}
		return val, nil

	case types.Object:
		attrs := v.Attributes()
		newAttrs := make(map[string]attr.Value, len(attrs))
		newTypes := make(map[string]attr.Type, len(attrs))
		for k, a := range attrs {
			resolved, err := resolveValue(ctxMap, a)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}
			newAttrs[k] = types.DynamicValue(resolved)
			newTypes[k] = types.DynamicType
		}
		obj, diags := types.ObjectValue(newTypes, newAttrs)
		if diags.HasError() {
			return nil, fmt.Errorf("rebuilding object: %s", diags.Errors())
		}
		return obj, nil

	case types.Map:
		elems := v.Elements()
		newElems := make(map[string]attr.Value, len(elems))
		for k, e := range elems {
			resolved, err := resolveValue(ctxMap, e)
			if err != nil {
				return nil, fmt.Errorf("map key %q: %w", k, err)
			}
			newElems[k] = resolved
		}
		m, diags := types.MapValue(types.DynamicType, wrapDynamic(newElems))
		if diags.HasError() {
			allStrings := true
			for _, e := range newElems {
				if _, ok := e.(types.String); !ok {
					allStrings = false
					break
				}
			}
			if allStrings {
				m, diags = types.MapValue(types.StringType, newElems)
				if diags.HasError() {
					return nil, fmt.Errorf("rebuilding map: %s", diags.Errors())
				}
				return m, nil
			}
			return nil, fmt.Errorf("rebuilding map: %s", diags.Errors())
		}
		return m, nil

	case types.Tuple:
		elems := v.Elements()
		newElems := make([]attr.Value, len(elems))
		newTypes := make([]attr.Type, len(elems))
		for i, e := range elems {
			resolved, err := resolveValue(ctxMap, e)
			if err != nil {
				return nil, fmt.Errorf("tuple index %d: %w", i, err)
			}
			newElems[i] = resolved
			if resolved != nil && !resolved.IsNull() {
				newTypes[i] = resolved.Type(context.Background())
			} else {
				newTypes[i] = e.Type(context.Background())
			}
		}
		t, diags := types.TupleValue(newTypes, newElems)
		if diags.HasError() {
			return nil, fmt.Errorf("rebuilding tuple: %s", diags.Errors())
		}
		return t, nil

	case types.List:
		elems := v.Elements()
		newElems := make([]attr.Value, len(elems))
		for i, e := range elems {
			resolved, err := resolveValue(ctxMap, e)
			if err != nil {
				return nil, fmt.Errorf("list index %d: %w", i, err)
			}
			newElems[i] = resolved
		}
		l, diags := types.ListValue(v.ElementType(context.Background()), newElems)
		if diags.HasError() {
			dynElems := wrapDynamicList(newElems)
			l, diags = types.ListValue(types.DynamicType, dynElems)
			if diags.HasError() {
				return nil, fmt.Errorf("rebuilding list: %s", diags.Errors())
			}
		}
		return l, nil

	case types.Dynamic:
		return resolveValue(ctxMap, v.UnderlyingValue())

	default:
		return val, nil
	}
}

// resolveInterpolatedRef handles strings containing ${ref:...} expressions
// embedded in a larger string. Each ${ref:path} is resolved and its string
// value is substituted inline. Non-string resolved values cause an error.
func resolveInterpolatedRef(ctxMap attr.Value, s string) (attr.Value, error) {
	var result strings.Builder
	remaining := s

	for {
		start := strings.Index(remaining, "${ref:")
		if start < 0 {
			result.WriteString(remaining)
			break
		}
		result.WriteString(remaining[:start])

		inner := remaining[start+2:] // skip "${"
		end := strings.Index(inner, "}")
		if end < 0 {
			return nil, fmt.Errorf("unterminated ${ref:...} in %q", s)
		}

		refExpr := inner[:end] // "ref:path" or "ref:path|default"
		resolved, err := resolveSingleRef(ctxMap, refExpr)
		if err != nil {
			return nil, fmt.Errorf("interpolation in %q: %w", s, err)
		}

		strVal, ok := resolved.(types.String)
		if !ok {
			return nil, fmt.Errorf("interpolation ${%s} in %q resolved to non-string type %T", refExpr, s, resolved)
		}
		result.WriteString(strVal.ValueString())

		remaining = inner[end+1:]
	}

	return types.StringValue(result.String()), nil
}

func resolveSingleRef(ctxMap attr.Value, ref string) (attr.Value, error) {
	path := strings.TrimPrefix(ref, "ref:")

	var defaultVal *string
	if idx := strings.Index(path, "|"); idx >= 0 {
		d := path[idx+1:]
		defaultVal = &d
		path = path[:idx]
	}

	segments := strings.Split(path, ".")
	resolved, err := walkDynamic(ctxMap, segments)
	if err != nil {
		if defaultVal != nil {
			return types.StringValue(*defaultVal), nil
		}
		hint := ""
		if len(segments) > 0 && segments[0] == "remote_states" {
			hint = "\n\nHint: remote_states is empty. Pass -var='remote_state_backend={...}' and -var='remote_state_keys={...}' to populate it."
		}
		return nil, fmt.Errorf("%s - %s%s", ref, err.Error(), hint)
	}
	return resolved, nil
}

func wrapDynamic(m map[string]attr.Value) map[string]attr.Value {
	out := make(map[string]attr.Value, len(m))
	for k, v := range m {
		out[k] = types.DynamicValue(v)
	}
	return out
}

func wrapDynamicList(elems []attr.Value) []attr.Value {
	out := make([]attr.Value, len(elems))
	for i, v := range elems {
		out[i] = types.DynamicValue(v)
	}
	return out
}
